/*
 * airtune_sync.cpp — System-wide IAudioClock::GetPosition() hook for A/V sync.
 *
 * Loaded into processes via:
 * 1. SetWindowsHookEx(WH_GETMESSAGE) — for GUI processes
 * 2. CreateRemoteThread(LoadLibraryW) — for sandboxed processes (Chrome audio)
 *
 * On load, spawns an init thread that patches IAudioClient vtable
 * so any future GetService(IAudioClock) returns a hooked IAudioClock
 * whose GetPosition() subtracts the AirPlay latency.
 */
#define WIN32_LEAN_AND_MEAN
#define COBJMACROS
#include <windows.h>
#include <mmdeviceapi.h>
#include <audioclient.h>
#include <functiondiscoverykeys_devpkey.h>
#include <initguid.h>
#include <stdio.h>

#include "airtune_sync.h"

/* ── Debug logging ── */

static void DbgLog(const char* fmt, ...)
{
    char path[MAX_PATH];
    GetTempPathA(MAX_PATH, path);
    strcat_s(path, "airtune_sync.log");

    FILE* f = NULL;
    fopen_s(&f, path, "a");
    if (!f) return;

    DWORD pid = GetCurrentProcessId();
    char exeName[MAX_PATH] = {0};
    GetModuleFileNameA(NULL, exeName, MAX_PATH);
    char* lastSlash = strrchr(exeName, '\\');
    if (lastSlash) memmove(exeName, lastSlash + 1, strlen(lastSlash));

    fprintf(f, "[%lu|%s] ", pid, exeName);

    va_list args;
    va_start(args, fmt);
    vfprintf(f, fmt, args);
    va_end(args);

    fprintf(f, "\n");
    fclose(f);
}

/* ── Globals ── */

static AirTuneSyncData* g_SyncData    = NULL;
static HANDLE           g_hMapping    = NULL;
static LONG             g_Initialized = 0;
static HMODULE          g_hSelf       = NULL;

typedef HRESULT (STDMETHODCALLTYPE *GetServiceFn)(IAudioClient*, REFIID, void**);
typedef HRESULT (STDMETHODCALLTYPE *GetPositionFn)(IAudioClock*, UINT64*, UINT64*);
typedef HRESULT (STDMETHODCALLTYPE *GetFrequencyFn)(IAudioClock*, UINT64*);

static GetServiceFn  g_OrigGetService  = NULL;
static GetPositionFn g_OrigGetPosition = NULL;

/* ── Hooked GetPosition ── */

static LONG g_GetPositionCalls = 0;

static HRESULT STDMETHODCALLTYPE HookedGetPosition(
    IAudioClock* pThis, UINT64* pu64Position, UINT64* pu64QPCPosition)
{
    HRESULT hr = g_OrigGetPosition(pThis, pu64Position, pu64QPCPosition);
    if (FAILED(hr) || !pu64Position || !g_SyncData)
        return hr;

    if (!g_SyncData->enabled || g_SyncData->latency_hns <= 0)
        return hr;

    UINT64 freq = 0;
    void** vtable = *(void***)pThis;
    GetFrequencyFn getFreq = (GetFrequencyFn)vtable[3];
    if (FAILED(getFreq(pThis, &freq)) || freq == 0)
        return hr;

    INT64 latHns = (INT64)g_SyncData->latency_hns;
    INT64 delayUnits = (INT64)((latHns * (INT64)freq) / 10000000LL);

    UINT64 origPos = *pu64Position;
    if ((INT64)*pu64Position > delayUnits)
        *pu64Position -= (UINT64)delayUnits;
    else
        *pu64Position = 0;

    LONG calls = InterlockedIncrement(&g_GetPositionCalls);
    if (calls <= 5 || (calls % 1000) == 0) {
        DbgLog("GetPosition: orig=%llu adjusted=%llu freq=%llu delay=%lld (calls=%ld)",
               origPos, *pu64Position, freq, delayUnits, calls);
    }

    return hr;
}

/* ── Hooked GetService ── */

static void PatchAudioClockVtable(IAudioClock* pClock)
{
    void** vtable = *(void***)pClock;

    if (vtable[4] == (void*)HookedGetPosition) {
        DbgLog("PatchAudioClock: already patched");
        return;
    }

    GetPositionFn orig = (GetPositionFn)vtable[4];
    if (InterlockedCompareExchangePointer(
            (volatile PVOID*)&g_OrigGetPosition, (PVOID)orig, NULL) != NULL) {
        if (g_OrigGetPosition == (GetPositionFn)HookedGetPosition)
            return;
    }

    DWORD oldProtect = 0;
    if (VirtualProtect(&vtable[4], sizeof(void*), PAGE_READWRITE, &oldProtect)) {
        vtable[4] = (void*)HookedGetPosition;
        VirtualProtect(&vtable[4], sizeof(void*), oldProtect, &oldProtect);
        DbgLog("PatchAudioClock: SUCCESS — vtable[4] patched");
    } else {
        DbgLog("PatchAudioClock: FAILED — VirtualProtect error %lu", GetLastError());
    }
}

static HRESULT STDMETHODCALLTYPE HookedGetService(
    IAudioClient* pThis, REFIID riid, void** ppv)
{
    HRESULT hr = g_OrigGetService(pThis, riid, ppv);
    if (SUCCEEDED(hr) && ppv && *ppv && IsEqualIID(riid, __uuidof(IAudioClock))) {
        DbgLog("GetService(IAudioClock) intercepted — patching vtable");
        PatchAudioClockVtable((IAudioClock*)*ppv);
    }
    return hr;
}

/* ── Initialization (runs on a worker thread, safe for COM) ── */

static void InitializeAudioHook()
{
    DbgLog("InitializeAudioHook: starting");

    g_hMapping = OpenFileMappingW(FILE_MAP_READ, FALSE, AIRTUNE_SYNC_SHM_NAME);
    if (!g_hMapping) {
        DbgLog("InitializeAudioHook: OpenFileMapping FAILED (%lu)", GetLastError());
        return;
    }

    g_SyncData = (AirTuneSyncData*)MapViewOfFile(
        g_hMapping, FILE_MAP_READ, 0, 0, AIRTUNE_SYNC_SHM_SIZE);
    if (!g_SyncData || g_SyncData->magic != AIRTUNE_SYNC_MAGIC) {
        DbgLog("InitializeAudioHook: MapViewOfFile failed or bad magic");
        if (g_SyncData) { UnmapViewOfFile(g_SyncData); g_SyncData = NULL; }
        CloseHandle(g_hMapping); g_hMapping = NULL;
        return;
    }

    DbgLog("InitializeAudioHook: SHM opened — latency=%lldms enabled=%u",
           g_SyncData->latency_hns / 10000, g_SyncData->enabled);

    HRESULT hr = CoInitializeEx(NULL, COINIT_MULTITHREADED);
    bool needUninit = SUCCEEDED(hr);
    if (FAILED(hr) && hr != RPC_E_CHANGED_MODE) {
        hr = CoInitializeEx(NULL, COINIT_APARTMENTTHREADED);
        needUninit = SUCCEEDED(hr);
        if (FAILED(hr) && hr != RPC_E_CHANGED_MODE) {
            DbgLog("InitializeAudioHook: CoInitializeEx FAILED (0x%08x)", hr);
            return;
        }
    }

    IMMDeviceEnumerator* pEnum = NULL;
    hr = CoCreateInstance(__uuidof(MMDeviceEnumerator), NULL, CLSCTX_ALL,
                          __uuidof(IMMDeviceEnumerator), (void**)&pEnum);
    if (FAILED(hr)) {
        DbgLog("InitializeAudioHook: CoCreateInstance(MMDeviceEnumerator) FAILED (0x%08x)", hr);
        if (needUninit) CoUninitialize();
        return;
    }

    IMMDevice* pDevice = NULL;
    hr = pEnum->GetDefaultAudioEndpoint(eRender, eConsole, &pDevice);
    if (FAILED(hr)) {
        DbgLog("InitializeAudioHook: GetDefaultAudioEndpoint FAILED (0x%08x)", hr);
        pEnum->Release();
        if (needUninit) CoUninitialize();
        return;
    }

    IAudioClient* pClient = NULL;
    hr = pDevice->Activate(__uuidof(IAudioClient), CLSCTX_ALL,
                           NULL, (void**)&pClient);
    pDevice->Release();
    if (FAILED(hr)) {
        DbgLog("InitializeAudioHook: Activate(IAudioClient) FAILED (0x%08x)", hr);
        pEnum->Release();
        if (needUninit) CoUninitialize();
        return;
    }

    void** vtable = *(void***)pClient;
    g_OrigGetService = (GetServiceFn)vtable[14];

    DWORD oldProtect = 0;
    if (VirtualProtect(&vtable[14], sizeof(void*), PAGE_READWRITE, &oldProtect)) {
        vtable[14] = (void*)HookedGetService;
        VirtualProtect(&vtable[14], sizeof(void*), oldProtect, &oldProtect);
        DbgLog("InitializeAudioHook: IAudioClient vtable[14] (GetService) PATCHED");
    } else {
        DbgLog("InitializeAudioHook: VirtualProtect FAILED (%lu)", GetLastError());
    }

    pClient->Release();
    pEnum->Release();
    if (needUninit) CoUninitialize();

    DbgLog("InitializeAudioHook: DONE");
}

/* ── Init thread (spawned from DllMain, runs after loader lock releases) ── */

static DWORD WINAPI InitThread(LPVOID)
{
    /* Small delay to let the process finish initializing */
    Sleep(500);

    if (InterlockedCompareExchange(&g_Initialized, 1, 0) == 0) {
        InitializeAudioHook();
    }
    return 0;
}

/* ── Cleanup ── */

static void CleanupHook()
{
    if (g_SyncData) { UnmapViewOfFile(g_SyncData); g_SyncData = NULL; }
    if (g_hMapping) { CloseHandle(g_hMapping); g_hMapping = NULL; }
}

/* ── Exported hook procedure (for SetWindowsHookEx path) ── */

extern "C" __declspec(dllexport)
LRESULT CALLBACK AirTuneCBTProc(int nCode, WPARAM wParam, LPARAM lParam)
{
    /* Also trigger init from hook callback (for processes loaded via hook) */
    if (InterlockedCompareExchange(&g_Initialized, 1, 0) == 0) {
        InitializeAudioHook();
    }
    return CallNextHookEx(NULL, nCode, wParam, lParam);
}

/* ── DllMain ── */

BOOL APIENTRY DllMain(HMODULE hModule, DWORD reason, LPVOID)
{
    switch (reason) {
    case DLL_PROCESS_ATTACH:
        g_hSelf = hModule;
        DisableThreadLibraryCalls(hModule);
        /* Spawn init thread — runs after DllMain returns (loader lock released).
           This ensures initialization happens for BOTH hook-loaded and
           CreateRemoteThread-loaded scenarios. */
        {
            HANDLE hThread = CreateThread(NULL, 0, InitThread, NULL, 0, NULL);
            if (hThread) CloseHandle(hThread);
        }
        break;
    case DLL_PROCESS_DETACH:
        CleanupHook();
        break;
    }
    return TRUE;
}
