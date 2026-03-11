/*
 * stream.cpp — WaveRT stream: buffer mgmt, delayed notifications for A/V sync.
 *
 * A/V sync approach: the audio engine uses notification events as its clock.
 * By delaying when we fire notifications (by the AirPlay latency), the engine's
 * IAudioClock::GetPosition() genuinely lags behind wall-clock, causing video
 * renderers to hold frames. A large WaveRT buffer absorbs the delay.
 */
#pragma warning(disable: 4996)

#include "miniport.h"

/* Minimum WaveRT buffer: 4 seconds. Large enough to absorb notification delay
   without underruns. Memory is cheap for a virtual device. */
#define MIN_BUFFER_BYTES (4 * AIRTUNE_SAMPLE_RATE * AIRTUNE_BLOCK_ALIGN)

/* ── Constructor / Destructor ── */

CMiniportWaveRTStream::CMiniportWaveRTStream(PUNKNOWN OuterUnknown)
    : CUnknown(OuterUnknown)
    , m_pMiniport(NULL)
    , m_pPortStream(NULL)
    , m_pDmaBuffer(NULL)
    , m_DmaBufferSize(0)
    , m_pDmaMdl(NULL)
    , m_FramesConsumed(0)
    , m_FramesPlayed(0)
    , m_BlockAlign(AIRTUNE_BLOCK_ALIGN)
    , m_NotificationInterval(10)
    , m_PeriodFrames(0)
    , m_NotificationCount(0)
    , m_LastNotifiedFrame(0)
    , m_StreamActive(FALSE)
{
    m_LastPerfCounter.QuadPart = 0;
    m_PerfFrequency.QuadPart = 0;
    RtlZeroMemory(m_NotificationEvents, sizeof(m_NotificationEvents));
}

CMiniportWaveRTStream::~CMiniportWaveRTStream()
{
    if (m_StreamActive) {
        KeCancelTimer(&m_Timer);
        m_StreamActive = FALSE;
    }

    if (g_SharedMemory) {
        AIRTUNE_SHM_HEADER* hdr = (AIRTUNE_SHM_HEADER*)g_SharedMemory;
        InterlockedAnd((volatile LONG*)&hdr->Flags, ~AIRTUNE_FLAG_ACTIVE);
    }

    if (m_pPortStream) { m_pPortStream->Release(); m_pPortStream = NULL; }
    m_pMiniport = NULL;
}

STDMETHODIMP_(NTSTATUS) CMiniportWaveRTStream::NonDelegatingQueryInterface(
    REFIID Interface, PVOID* Object
)
{
    PAGED_CODE();
    ASSERT(Object);

    if (IsEqualGUIDAligned(Interface, IID_IUnknown))
        *Object = PVOID(PUNKNOWN(PMINIPORTWAVERTSTREAM(this)));
    else if (IsEqualGUIDAligned(Interface, IID_IMiniportWaveRTStream))
        *Object = PVOID(PMINIPORTWAVERTSTREAM(this));
    else if (IsEqualGUIDAligned(Interface, IID_IMiniportWaveRTStreamNotification))
        *Object = PVOID(PMINIPORTWAVERTSTREAMNOTIFICATION(this));
    else {
        *Object = NULL;
        return STATUS_INVALID_PARAMETER;
    }

    PUNKNOWN(*Object)->AddRef();
    return STATUS_SUCCESS;
}

NTSTATUS CMiniportWaveRTStream::Init(
    CMiniportWaveRT* Miniport, PPORTWAVERTSTREAM PortStream
)
{
    PAGED_CODE();
    m_pMiniport = Miniport;
    m_pPortStream = PortStream;
    m_pPortStream->AddRef();

    KeQueryPerformanceCounter(&m_PerfFrequency);
    m_LastPerfCounter = KeQueryPerformanceCounter(NULL);

    KeInitializeTimer(&m_Timer);
    KeInitializeDpc(&m_Dpc, TimerDpcRoutine, this);

    return STATUS_SUCCESS;
}

/* ── SetFormat / SetState ── */

#pragma code_seg("PAGE")
STDMETHODIMP_(NTSTATUS) CMiniportWaveRTStream::SetFormat(PKSDATAFORMAT DataFormat)
{
    PAGED_CODE();
    UNREFERENCED_PARAMETER(DataFormat);
    return STATUS_SUCCESS;
}

STDMETHODIMP_(NTSTATUS) CMiniportWaveRTStream::SetState(KSSTATE State)
{
    PAGED_CODE();

    switch (State) {
    case KSSTATE_RUN:
        if (!m_StreamActive) {
            m_StreamActive = TRUE;
            m_LastPerfCounter = KeQueryPerformanceCounter(NULL);
            m_LastNotifiedFrame = 0;

            if (g_SharedMemory) {
                AIRTUNE_SHM_HEADER* hdr = (AIRTUNE_SHM_HEADER*)g_SharedMemory;
                InterlockedOr((volatile LONG*)&hdr->Flags, AIRTUNE_FLAG_ACTIVE);
            }

            /* DPC fires every 10ms — fast enough for responsive notifications
               while decoupled from the notification period. */
            LARGE_INTEGER dueTime;
            dueTime.QuadPart = -((LONG64)m_NotificationInterval * 10000LL);
            KeSetTimerEx(&m_Timer, dueTime, m_NotificationInterval, &m_Dpc);
        }
        break;

    case KSSTATE_PAUSE:
    case KSSTATE_ACQUIRE:
    case KSSTATE_STOP:
        if (m_StreamActive) {
            KeCancelTimer(&m_Timer);
            m_StreamActive = FALSE;

            if (g_SharedMemory) {
                AIRTUNE_SHM_HEADER* hdr = (AIRTUNE_SHM_HEADER*)g_SharedMemory;
                InterlockedAnd((volatile LONG*)&hdr->Flags, ~AIRTUNE_FLAG_ACTIVE);
            }
        }
        if (State == KSSTATE_STOP) {
            InterlockedExchange64(&m_FramesConsumed, 0);
            InterlockedExchange64(&m_FramesPlayed, 0);
            m_LastNotifiedFrame = 0;
        }
        break;
    }

    return STATUS_SUCCESS;
}
#pragma code_seg()

/* ── Buffer Allocation ── */

#pragma code_seg("PAGE")
STDMETHODIMP_(NTSTATUS) CMiniportWaveRTStream::AllocateAudioBuffer(
    ULONG RequestedSize, PMDL* AudioBufferMdl,
    ULONG* ActualSize, ULONG* OffsetFromFirstPage,
    MEMORY_CACHING_TYPE* CacheType
)
{
    PAGED_CODE();

    /* Allocate at least MIN_BUFFER_BYTES (4 seconds) so we can absorb
       notification delays without the audio engine running out of write space. */
    ULONG allocSize = max(RequestedSize, MIN_BUFFER_BYTES);
    allocSize = (allocSize + PAGE_SIZE - 1) & ~(PAGE_SIZE - 1);

    m_pDmaBuffer = ExAllocatePool2(POOL_FLAG_NON_PAGED, allocSize, 'BrtA');
    if (!m_pDmaBuffer) return STATUS_INSUFFICIENT_RESOURCES;
    RtlZeroMemory(m_pDmaBuffer, allocSize);
    m_DmaBufferSize = allocSize;

    m_pDmaMdl = IoAllocateMdl(m_pDmaBuffer, allocSize, FALSE, FALSE, NULL);
    if (!m_pDmaMdl) {
        ExFreePoolWithTag(m_pDmaBuffer, 'BrtA');
        m_pDmaBuffer = NULL;
        return STATUS_INSUFFICIENT_RESOURCES;
    }
    MmBuildMdlForNonPagedPool(m_pDmaMdl);

    *AudioBufferMdl = m_pDmaMdl;
    *ActualSize = allocSize;
    *OffsetFromFirstPage = 0;
    *CacheType = MmCached;
    return STATUS_SUCCESS;
}

STDMETHODIMP_(VOID) CMiniportWaveRTStream::FreeAudioBuffer(
    PMDL AudioBufferMdl, ULONG BufferSize
)
{
    PAGED_CODE();
    UNREFERENCED_PARAMETER(BufferSize);
    if (AudioBufferMdl) IoFreeMdl(AudioBufferMdl);
    if (m_pDmaBuffer) { ExFreePoolWithTag(m_pDmaBuffer, 'BrtA'); m_pDmaBuffer = NULL; }
    m_pDmaMdl = NULL;
    m_DmaBufferSize = 0;
}
#pragma code_seg()

/* ── Buffer with Notification ── */

#pragma code_seg("PAGE")
STDMETHODIMP_(NTSTATUS) CMiniportWaveRTStream::AllocateBufferWithNotification(
    ULONG NotificationCount, ULONG RequestedSize,
    PMDL* AudioBufferMdl, ULONG* ActualSize,
    ULONG* OffsetFromFirstPage, MEMORY_CACHING_TYPE* CacheType
)
{
    PAGED_CODE();

    NTSTATUS status = AllocateAudioBuffer(
        RequestedSize, AudioBufferMdl, ActualSize, OffsetFromFirstPage, CacheType
    );

    if (NT_SUCCESS(status) && NotificationCount > 0) {
        /* Period = buffer frames / notification count.
           The engine writes one period per notification event. */
        ULONG bufferFrames = m_DmaBufferSize / m_BlockAlign;
        m_PeriodFrames = bufferFrames / NotificationCount;

        /* DPC always at 10ms for responsiveness */
        m_NotificationInterval = 10;
    }

    return status;
}

STDMETHODIMP_(VOID) CMiniportWaveRTStream::FreeBufferWithNotification(
    PMDL AudioBufferMdl, ULONG BufferSize
)
{
    PAGED_CODE();
    FreeAudioBuffer(AudioBufferMdl, BufferSize);
}
#pragma code_seg()

/* ── Notification Events ── */

#pragma code_seg("PAGE")
STDMETHODIMP_(NTSTATUS) CMiniportWaveRTStream::RegisterNotificationEvent(
    PKEVENT NotificationEvent
)
{
    PAGED_CODE();
    if (m_NotificationCount >= MAX_NOTIFICATION_EVENTS)
        return STATUS_INSUFFICIENT_RESOURCES;

    m_NotificationEvents[m_NotificationCount++] = NotificationEvent;
    return STATUS_SUCCESS;
}

STDMETHODIMP_(NTSTATUS) CMiniportWaveRTStream::UnregisterNotificationEvent(
    PKEVENT NotificationEvent
)
{
    PAGED_CODE();
    for (ULONG i = 0; i < m_NotificationCount; i++) {
        if (m_NotificationEvents[i] == NotificationEvent) {
            for (ULONG j = i; j < m_NotificationCount - 1; j++)
                m_NotificationEvents[j] = m_NotificationEvents[j + 1];
            m_NotificationCount--;
            m_NotificationEvents[m_NotificationCount] = NULL;
            break;
        }
    }
    return STATUS_SUCCESS;
}
#pragma code_seg()

/* ── Position Reporting ── */

STDMETHODIMP_(NTSTATUS) CMiniportWaveRTStream::GetPosition(KSAUDIO_POSITION* Position)
{
    if (!Position) return STATUS_INVALID_PARAMETER;

    LONG64 played = InterlockedCompareExchange64(&m_FramesPlayed, 0, 0);
    LONG64 consumed = InterlockedCompareExchange64(&m_FramesConsumed, 0, 0);

    Position->PlayOffset  = (UINT64)played * m_BlockAlign;
    Position->WriteOffset = (UINT64)consumed * m_BlockAlign;
    return STATUS_SUCCESS;
}

/* ── HW Latency / Registers ── */

#pragma code_seg("PAGE")
STDMETHODIMP_(VOID) CMiniportWaveRTStream::GetHWLatency(KSRTAUDIO_HWLATENCY* Latency)
{
    PAGED_CODE();
    Latency->ChipsetDelay = 0;
    Latency->CodecDelay   = 0;
    Latency->FifoSize     = 0;
}
#pragma code_seg()

STDMETHODIMP_(NTSTATUS) CMiniportWaveRTStream::GetClockRegister(KSRTAUDIO_HWREGISTER* Register)
{
    UNREFERENCED_PARAMETER(Register);
    return STATUS_NOT_SUPPORTED;
}

STDMETHODIMP_(NTSTATUS) CMiniportWaveRTStream::GetPositionRegister(KSRTAUDIO_HWREGISTER* Register)
{
    UNREFERENCED_PARAMETER(Register);
    return STATUS_NOT_SUPPORTED;
}

/* ── DPC Timer ── */

void NTAPI CMiniportWaveRTStream::TimerDpcRoutine(
    PKDPC Dpc, PVOID DeferredContext, PVOID, PVOID
)
{
    UNREFERENCED_PARAMETER(Dpc);
    CMiniportWaveRTStream* stream = (CMiniportWaveRTStream*)DeferredContext;
    if (stream) stream->ProcessDpc();
}

void CMiniportWaveRTStream::ProcessDpc()
{
    if (!m_StreamActive || !m_pDmaBuffer) return;

    LARGE_INTEGER now = KeQueryPerformanceCounter(NULL);
    LONG64 deltaTicks = now.QuadPart - m_LastPerfCounter.QuadPart;
    m_LastPerfCounter = now;

    if (deltaTicks <= 0 || m_PerfFrequency.QuadPart == 0) return;

    ULONG newFrames = (ULONG)((deltaTicks * AIRTUNE_SAMPLE_RATE) / m_PerfFrequency.QuadPart);
    if (newFrames == 0) return;

    /* Advance actual consumed position at wall-clock rate */
    LONG64 prevConsumed = InterlockedCompareExchange64(&m_FramesConsumed, 0, 0);
    LONG64 totalConsumed = prevConsumed + newFrames;
    InterlockedExchange64(&m_FramesConsumed, totalConsumed);

    /* Delayed "played" position — lags behind by the AirPlay latency.
       This is what the audio engine sees via notifications + GetPosition(),
       causing IAudioClock::GetPosition() to genuinely report a delayed time. */
    UINT64 latencyFrames = m_pMiniport->GetLatencyFrames();
    LONG64 played = (totalConsumed > (LONG64)latencyFrames)
                    ? totalConsumed - (LONG64)latencyFrames : 0;
    InterlockedExchange64(&m_FramesPlayed, played);

    /* Fire notification events when the DELAYED position crosses period boundaries.
       The audio engine uses these as its clock — delaying them = delaying the clock. */
    if (m_PeriodFrames > 0 && m_NotificationCount > 0) {
        LONG64 currentPeriod = played / (LONG64)m_PeriodFrames;
        LONG64 lastPeriod = m_LastNotifiedFrame / (LONG64)m_PeriodFrames;

        if (currentPeriod > lastPeriod) {
            for (ULONG i = 0; i < m_NotificationCount; i++) {
                if (m_NotificationEvents[i])
                    KeSetEvent(m_NotificationEvents[i], IO_NO_INCREMENT, FALSE);
            }
            m_LastNotifiedFrame = played;
        }
    }

    /* Update global frame counter (for IOCTL status) */
    InterlockedExchange64(&g_FramesWritten, totalConsumed);

    /* Copy from WaveRT buffer → shared memory ring buffer (using actual position) */
    if (g_SharedMemory && m_pDmaBuffer) {
        AIRTUNE_SHM_HEADER* hdr = (AIRTUNE_SHM_HEADER*)g_SharedMemory;
        BYTE* ringBuf = (BYTE*)g_SharedMemory + AIRTUNE_SHM_HEADER_SIZE;

        ULONG bytesToCopy = newFrames * m_BlockAlign;
        ULONG srcOffset = (ULONG)((prevConsumed * m_BlockAlign) % m_DmaBufferSize);
        ULONG dstOffset = (ULONG)((prevConsumed * m_BlockAlign) % AIRTUNE_RING_BUF_SIZE);

        ULONG remaining = bytesToCopy;
        while (remaining > 0) {
            ULONG srcChunk = min(remaining, m_DmaBufferSize - srcOffset);
            ULONG dstChunk = min(srcChunk, AIRTUNE_RING_BUF_SIZE - dstOffset);
            ULONG copyLen = min(srcChunk, dstChunk);

            RtlCopyMemory(ringBuf + dstOffset, (BYTE*)m_pDmaBuffer + srcOffset, copyLen);

            remaining -= copyLen;
            srcOffset = (srcOffset + copyLen) % m_DmaBufferSize;
            dstOffset = (dstOffset + copyLen) % AIRTUNE_RING_BUF_SIZE;
        }

        InterlockedExchange64(&hdr->WriteOffset, totalConsumed * m_BlockAlign);
        InterlockedExchange64(&hdr->FramesWritten, totalConsumed);

        if (g_DataReadyEvent)
            KeSetEvent(g_DataReadyEvent, IO_NO_INCREMENT, FALSE);
    }
}
