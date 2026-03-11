/*
 * adapter.cpp — DriverEntry, AddDevice, StartDevice for AirTune Virtual Audio.
 * MINIMAL VERSION: Just get the audio endpoint to appear.
 */
#pragma warning(disable: 4996)

#include "adapter.h"
#include "miniport.h"
#include "toptable.h"

/* ── Globals ── */
volatile LONG64  g_LatencyHns    = 0;
volatile LONG64  g_FramesWritten = 0;
PVOID            g_SharedMemory  = NULL;
PKEVENT          g_DataReadyEvent = NULL;

/* Control device object for IOCTL access */
static PDEVICE_OBJECT  g_ControlDevice = NULL;

/* Saved PortCls dispatch handlers — we chain to these for non-control devices */
static PDRIVER_DISPATCH g_PortClsCreate  = NULL;
static PDRIVER_DISPATCH g_PortClsClose   = NULL;
static PDRIVER_DISPATCH g_PortClsIoctl   = NULL;

/* Forward */
NTSTATUS StartDevice(PDEVICE_OBJECT, PIRP, PRESOURCELIST);

/* Diagnostic: write step+status to registry so we can see where StartDevice fails */
void DiagWrite(ULONG step, NTSTATUS status)
{
    HANDLE hKey = NULL;
    UNICODE_STRING keyPath = RTL_CONSTANT_STRING(
        L"\\Registry\\Machine\\SOFTWARE\\AirTune");
    OBJECT_ATTRIBUTES oa;
    InitializeObjectAttributes(&oa, &keyPath, OBJ_CASE_INSENSITIVE | OBJ_KERNEL_HANDLE, NULL, NULL);

    NTSTATUS ks = ZwCreateKey(&hKey, KEY_WRITE, &oa, 0, NULL, REG_OPTION_NON_VOLATILE, NULL);
    if (!NT_SUCCESS(ks)) return;

    WCHAR valName[32];
    /* "Step_XX" */
    valName[0] = L'S'; valName[1] = L't'; valName[2] = L'e'; valName[3] = L'p';
    valName[4] = L'_';
    valName[5] = L'0' + (WCHAR)((step / 10) % 10);
    valName[6] = L'0' + (WCHAR)(step % 10);
    valName[7] = L'\0';

    UNICODE_STRING valueName;
    RtlInitUnicodeString(&valueName, valName);
    ZwSetValueKey(hKey, &valueName, 0, REG_DWORD, &status, sizeof(ULONG));
    ZwClose(hKey);
}

/* Write a 64-bit diagnostic */
void DiagWrite64(ULONG step, LONG64 value)
{
    HANDLE hKey = NULL;
    UNICODE_STRING keyPath = RTL_CONSTANT_STRING(
        L"\\Registry\\Machine\\SOFTWARE\\AirTune");
    OBJECT_ATTRIBUTES oa;
    InitializeObjectAttributes(&oa, &keyPath, OBJ_CASE_INSENSITIVE | OBJ_KERNEL_HANDLE, NULL, NULL);

    NTSTATUS ks = ZwCreateKey(&hKey, KEY_WRITE, &oa, 0, NULL, REG_OPTION_NON_VOLATILE, NULL);
    if (!NT_SUCCESS(ks)) return;

    WCHAR valName[32];
    valName[0] = L'D'; valName[1] = L'_';
    valName[2] = L'0' + (WCHAR)((step / 10) % 10);
    valName[3] = L'0' + (WCHAR)(step % 10);
    valName[4] = L'\0';

    UNICODE_STRING valueName;
    RtlInitUnicodeString(&valueName, valName);
    ULONG dw = (ULONG)value;
    ZwSetValueKey(hKey, &valueName, 0, REG_DWORD, &dw, sizeof(ULONG));
    ZwClose(hKey);
}

/* ── AddDevice ── */

#pragma code_seg("PAGE")
NTSTATUS AddDevice(PDRIVER_OBJECT DriverObject, PDEVICE_OBJECT PhysicalDeviceObject)
{
    PAGED_CODE();
    return PcAddAdapterDevice(DriverObject, PhysicalDeviceObject, StartDevice, 3, 0);
}
#pragma code_seg()

/* ── StartDevice ── */

#pragma code_seg("PAGE")
NTSTATUS StartDevice(PDEVICE_OBJECT DeviceObject, PIRP Irp, PRESOURCELIST ResourceList)
{
    PAGED_CODE();
    UNREFERENCED_PARAMETER(ResourceList);
    NTSTATUS status;

    DiagWrite(0, 0xAAAAAAAA); /* marker: StartDevice entered */

    /* ── Wave subdevice ── */
    PPORT portWave = NULL;
    status = PcNewPort(&portWave, CLSID_PortWaveRT);
    DiagWrite(1, status); /* PcNewPort WaveRT */
    if (!NT_SUCCESS(status)) return status;

    PUNKNOWN unknownMiniportWave = NULL;
    status = CreateMiniportWaveRT(&unknownMiniportWave, CLSID_NULL, NULL, NonPagedPoolNx);
    DiagWrite(2, status); /* CreateMiniportWaveRT */
    if (!NT_SUCCESS(status)) { portWave->Release(); return status; }

    status = portWave->Init(DeviceObject, Irp, unknownMiniportWave, NULL, NULL);
    DiagWrite(3, status); /* portWave->Init */
    if (!NT_SUCCESS(status)) {
        unknownMiniportWave->Release();
        portWave->Release();
        return status;
    }

    status = PcRegisterSubdevice(DeviceObject, L"Wave", PUNKNOWN(portWave));
    DiagWrite(4, status); /* PcRegisterSubdevice Wave */
    if (!NT_SUCCESS(status)) {
        unknownMiniportWave->Release();
        portWave->Release();
        return status;
    }

    /* ── Topology subdevice ── */
    PPORT portTopo = NULL;
    status = PcNewPort(&portTopo, CLSID_PortTopology);
    DiagWrite(5, status); /* PcNewPort Topology */
    if (!NT_SUCCESS(status)) {
        unknownMiniportWave->Release();
        portWave->Release();
        return status;
    }

    PUNKNOWN unknownMiniportTopo = NULL;
    status = CreateMiniportTopology(&unknownMiniportTopo, CLSID_NULL, NULL, NonPagedPoolNx);
    DiagWrite(6, status); /* CreateMiniportTopology */
    if (!NT_SUCCESS(status)) {
        portTopo->Release();
        unknownMiniportWave->Release();
        portWave->Release();
        return status;
    }

    status = portTopo->Init(DeviceObject, Irp, unknownMiniportTopo, NULL, NULL);
    DiagWrite(7, status); /* portTopo->Init */
    if (!NT_SUCCESS(status)) {
        unknownMiniportTopo->Release();
        portTopo->Release();
        unknownMiniportWave->Release();
        portWave->Release();
        return status;
    }

    status = PcRegisterSubdevice(DeviceObject, L"Topology", PUNKNOWN(portTopo));
    DiagWrite(8, status); /* PcRegisterSubdevice Topology */
    if (!NT_SUCCESS(status)) {
        unknownMiniportTopo->Release();
        portTopo->Release();
        unknownMiniportWave->Release();
        portWave->Release();
        return status;
    }

    /* ── Physical connection: Wave bridge pin 1 → Topology input pin 0 ── */
    status = PcRegisterPhysicalConnection(
        DeviceObject,
        PUNKNOWN(portWave), 1,   /* from wave bridge output pin 1 */
        PUNKNOWN(portTopo), 0    /* to topology input pin 0 */
    );
    DiagWrite(9, status); /* PcRegisterPhysicalConnection */

    unknownMiniportTopo->Release();
    portTopo->Release();
    unknownMiniportWave->Release();
    portWave->Release();

    return status;
}
#pragma code_seg()

/* ── Chaining dispatch: CREATE ── */

static NTSTATUS AirTuneChainCreate(PDEVICE_OBJECT DeviceObject, PIRP Irp)
{
    if (DeviceObject == g_ControlDevice) {
        Irp->IoStatus.Status = STATUS_SUCCESS;
        Irp->IoStatus.Information = 0;
        IoCompleteRequest(Irp, IO_NO_INCREMENT);
        return STATUS_SUCCESS;
    }
    return g_PortClsCreate ? g_PortClsCreate(DeviceObject, Irp) : STATUS_INVALID_DEVICE_REQUEST;
}

/* ── Chaining dispatch: CLOSE ── */

static NTSTATUS AirTuneChainClose(PDEVICE_OBJECT DeviceObject, PIRP Irp)
{
    if (DeviceObject == g_ControlDevice) {
        Irp->IoStatus.Status = STATUS_SUCCESS;
        Irp->IoStatus.Information = 0;
        IoCompleteRequest(Irp, IO_NO_INCREMENT);
        return STATUS_SUCCESS;
    }
    return g_PortClsClose ? g_PortClsClose(DeviceObject, Irp) : STATUS_INVALID_DEVICE_REQUEST;
}

/* ── Chaining dispatch: DEVICE_CONTROL ── */

static NTSTATUS AirTuneChainIoctl(PDEVICE_OBJECT DeviceObject, PIRP Irp)
{
    if (DeviceObject == g_ControlDevice) {
        return AirTuneIoctlDispatch(DeviceObject, Irp);
    }
    return g_PortClsIoctl ? g_PortClsIoctl(DeviceObject, Irp) : STATUS_INVALID_DEVICE_REQUEST;
}

/* ── Unload: clean up control device ── */

static PDRIVER_UNLOAD g_PortClsUnload = NULL;

static void AirTuneUnload(PDRIVER_OBJECT DriverObject)
{
    if (g_ControlDevice) {
        UNICODE_STRING symlink = RTL_CONSTANT_STRING(AIRTUNE_SYMLINK_NAME);
        IoDeleteSymbolicLink(&symlink);
        IoDeleteDevice(g_ControlDevice);
        g_ControlDevice = NULL;
    }
    if (g_PortClsUnload) {
        g_PortClsUnload(DriverObject);
    }
}

/* ── DriverEntry ── */

#pragma code_seg("INIT")
extern "C" NTSTATUS DriverEntry(PDRIVER_OBJECT DriverObject, PUNICODE_STRING RegistryPath)
{
    /* 1. Let PortCls set up its dispatch table */
    NTSTATUS status = PcInitializeAdapterDriver(DriverObject, RegistryPath, (PDRIVER_ADD_DEVICE)AddDevice);
    if (!NT_SUCCESS(status)) return status;

    /* 2. Save PortCls dispatch handlers */
    g_PortClsCreate = DriverObject->MajorFunction[IRP_MJ_CREATE];
    g_PortClsClose  = DriverObject->MajorFunction[IRP_MJ_CLOSE];
    g_PortClsIoctl  = DriverObject->MajorFunction[IRP_MJ_DEVICE_CONTROL];

    /* 3. Install chaining handlers */
    DriverObject->MajorFunction[IRP_MJ_CREATE]         = AirTuneChainCreate;
    DriverObject->MajorFunction[IRP_MJ_CLOSE]          = AirTuneChainClose;
    DriverObject->MajorFunction[IRP_MJ_DEVICE_CONTROL] = AirTuneChainIoctl;

    /* 4. Chain unload */
    g_PortClsUnload = DriverObject->DriverUnload;
    DriverObject->DriverUnload = AirTuneUnload;

    /* 5. Create control device object for IOCTL access */
    UNICODE_STRING devName = RTL_CONSTANT_STRING(AIRTUNE_DEVICE_NAME);
    status = IoCreateDevice(
        DriverObject,
        0,                          /* no device extension */
        &devName,
        FILE_DEVICE_UNKNOWN,
        FILE_DEVICE_SECURE_OPEN,
        FALSE,                      /* not exclusive */
        &g_ControlDevice
    );

    if (NT_SUCCESS(status)) {
        UNICODE_STRING symlink = RTL_CONSTANT_STRING(AIRTUNE_SYMLINK_NAME);
        status = IoCreateSymbolicLink(&symlink, &devName);
        if (!NT_SUCCESS(status)) {
            IoDeleteDevice(g_ControlDevice);
            g_ControlDevice = NULL;
            /* Non-fatal: driver still works, just no IOCTL */
        } else {
            /* Mark control device initialized */
            g_ControlDevice->Flags &= ~DO_DEVICE_INITIALIZING;
        }
    } else {
        g_ControlDevice = NULL;
        /* Non-fatal */
    }

    return STATUS_SUCCESS;  /* Always succeed — IOCTL is optional */
}
#pragma code_seg()
