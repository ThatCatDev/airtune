/*
 * miniport.h — WaveRT miniport declarations for AirTune Virtual Audio.
 */
#pragma once

#include "adapter.h"

/* ── Filter descriptor (defined in miniport.cpp) ── */

extern PCFILTER_DESCRIPTOR g_WaveFilterDescriptor;

/* ── CMiniportWaveRT ── */

class CMiniportWaveRT :
    public IMiniportWaveRT,
    public CUnknown
{
public:
    DECLARE_STD_UNKNOWN();

    CMiniportWaveRT(PUNKNOWN OuterUnknown) : CUnknown(OuterUnknown), m_Port(NULL) {}
    ~CMiniportWaveRT();

    IMP_IMiniportWaveRT;

    UINT64 GetLatencyFrames() {
        LONG64 hns = InterlockedCompareExchange64(&g_LatencyHns, 0, 0);
        if (hns <= 0) return 0;
        return (UINT64)((hns * AIRTUNE_SAMPLE_RATE) / 10000000LL);
    }

private:
    PPORTWAVERT m_Port;
};

/* ── CMiniportWaveRTStream ── */

class CMiniportWaveRTStream :
    public IMiniportWaveRTStreamNotification,
    public CUnknown
{
public:
    DECLARE_STD_UNKNOWN();

    CMiniportWaveRTStream(PUNKNOWN OuterUnknown);
    ~CMiniportWaveRTStream();

    NTSTATUS Init(CMiniportWaveRT* Miniport, PPORTWAVERTSTREAM PortStream);

    IMP_IMiniportWaveRTStream;
    IMP_IMiniportWaveRTStreamNotification;

private:
    CMiniportWaveRT*     m_pMiniport;
    PPORTWAVERTSTREAM    m_pPortStream;

    PVOID                m_pDmaBuffer;
    ULONG                m_DmaBufferSize;
    PMDL                 m_pDmaMdl;

    volatile LONG64      m_FramesConsumed;   // actual wall-clock frames consumed
    volatile LONG64      m_FramesPlayed;     // delayed frames (notifications + GetPosition)
    ULONG                m_BlockAlign;
    LARGE_INTEGER        m_LastPerfCounter;
    LARGE_INTEGER        m_PerfFrequency;

    KTIMER               m_Timer;
    KDPC                 m_Dpc;
    ULONG                m_NotificationInterval; // DPC timer interval in ms (always 10ms)
    ULONG                m_PeriodFrames;         // frames per notification period

    static const ULONG   MAX_NOTIFICATION_EVENTS = 8;
    PKEVENT              m_NotificationEvents[MAX_NOTIFICATION_EVENTS];
    ULONG                m_NotificationCount;
    LONG64               m_LastNotifiedFrame;    // delayed frame count at last notification

    BOOL                 m_StreamActive;

    static void NTAPI TimerDpcRoutine(PKDPC Dpc, PVOID Context, PVOID, PVOID);
    void ProcessDpc();
};
