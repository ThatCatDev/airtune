/*
 * miniport.cpp — WaveRT miniport for AirTune Virtual Audio.
 */
#pragma warning(disable: 4996)

#include "miniport.h"

/* ── Audio format descriptors ── */

static KSDATARANGE_AUDIO g_WaveDataRangeRender = {
    {
        sizeof(KSDATARANGE_AUDIO), 0, 0, 0,
        STATICGUIDOF(KSDATAFORMAT_TYPE_AUDIO),
        STATICGUIDOF(KSDATAFORMAT_SUBTYPE_PCM),
        STATICGUIDOF(KSDATAFORMAT_SPECIFIER_WAVEFORMATEX)
    },
    AIRTUNE_CHANNELS,
    AIRTUNE_BIT_DEPTH, AIRTUNE_BIT_DEPTH,
    AIRTUNE_SAMPLE_RATE, AIRTUNE_SAMPLE_RATE
};

static PKSDATARANGE g_WaveDataRangePointersRender[] = {
    PKSDATARANGE(&g_WaveDataRangeRender)
};

static PCPIN_DESCRIPTOR g_WavePinDescriptors[] = {
    /* Pin 0: Host pin — audio engine writes here */
    {
        1, 1, 0,
        NULL,
        {
            0, NULL,
            0, NULL,
            SIZEOF_ARRAY(g_WaveDataRangePointersRender),
            g_WaveDataRangePointersRender,
            KSPIN_DATAFLOW_IN,
            KSPIN_COMMUNICATION_SINK,
            &KSCATEGORY_AUDIO,
            NULL,
            { 0 }
        }
    },
    /* Pin 1: Bridge pin — "physical" output to topology */
    {
        0, 0, 0,
        NULL,
        {
            0, NULL,
            0, NULL,
            SIZEOF_ARRAY(g_WaveDataRangePointersRender),
            g_WaveDataRangePointersRender,
            KSPIN_DATAFLOW_OUT,
            KSPIN_COMMUNICATION_NONE,
            &KSCATEGORY_AUDIO,
            NULL,
            { 0 }
        }
    }
};

/* Internal connection: host sink pin 0 -> bridge output pin 1 */
static PCCONNECTION_DESCRIPTOR g_WaveConnections[] = {
    { PCFILTER_NODE, 0,  PCFILTER_NODE, 1 }
};

PCFILTER_DESCRIPTOR g_WaveFilterDescriptor = {
    0, NULL,
    sizeof(PCPIN_DESCRIPTOR),
    SIZEOF_ARRAY(g_WavePinDescriptors),
    g_WavePinDescriptors,
    0, 0, NULL,    /* no nodes */
    SIZEOF_ARRAY(g_WaveConnections),
    g_WaveConnections,
    0, NULL
};

/* ── CMiniportWaveRT ── */

CMiniportWaveRT::~CMiniportWaveRT()
{
    if (m_Port) { m_Port->Release(); m_Port = NULL; }
}

STDMETHODIMP_(NTSTATUS) CMiniportWaveRT::NonDelegatingQueryInterface(
    REFIID Interface, PVOID* Object
)
{
    PAGED_CODE();
    ASSERT(Object);

    if (IsEqualGUIDAligned(Interface, IID_IUnknown))
        *Object = PVOID(PUNKNOWN(PMINIPORTWAVERT(this)));
    else if (IsEqualGUIDAligned(Interface, IID_IMiniport))
        *Object = PVOID(PMINIPORT(this));
    else if (IsEqualGUIDAligned(Interface, IID_IMiniportWaveRT))
        *Object = PVOID(PMINIPORTWAVERT(this));
    else {
        *Object = NULL;
        return STATUS_INVALID_PARAMETER;
    }

    PUNKNOWN(*Object)->AddRef();
    return STATUS_SUCCESS;
}

#pragma code_seg("PAGE")
STDMETHODIMP_(NTSTATUS) CMiniportWaveRT::GetDescription(
    PPCFILTER_DESCRIPTOR* Description
)
{
    PAGED_CODE();
    *Description = &g_WaveFilterDescriptor;
    return STATUS_SUCCESS;
}

STDMETHODIMP_(NTSTATUS) CMiniportWaveRT::DataRangeIntersection(
    ULONG, PKSDATARANGE, PKSDATARANGE, ULONG, PVOID, PULONG
)
{
    PAGED_CODE();
    return STATUS_NOT_IMPLEMENTED;
}

STDMETHODIMP_(NTSTATUS) CMiniportWaveRT::Init(
    PUNKNOWN UnknownAdapter, PRESOURCELIST ResourceList, PPORTWAVERT Port
)
{
    PAGED_CODE();
    UNREFERENCED_PARAMETER(UnknownAdapter);
    UNREFERENCED_PARAMETER(ResourceList);
    m_Port = Port;
    m_Port->AddRef();
    return STATUS_SUCCESS;
}

STDMETHODIMP_(NTSTATUS) CMiniportWaveRT::NewStream(
    PMINIPORTWAVERTSTREAM* OutStream,
    PPORTWAVERTSTREAM PortStream,
    ULONG Pin, BOOLEAN Capture, PKSDATAFORMAT DataFormat
)
{
    PAGED_CODE();
    UNREFERENCED_PARAMETER(Pin);
    UNREFERENCED_PARAMETER(Capture);
    UNREFERENCED_PARAMETER(DataFormat);

    DiagWrite(15, Pin); /* D_15: NewStream called for pin */

    CMiniportWaveRTStream* stream = new(NonPagedPoolNx, 'SrtA')
        CMiniportWaveRTStream(NULL);
    if (!stream) return STATUS_INSUFFICIENT_RESOURCES;

    NTSTATUS status = stream->Init(this, PortStream);
    if (!NT_SUCCESS(status)) { stream->Release(); return status; }

    *OutStream = PMINIPORTWAVERTSTREAM(stream);
    stream->AddRef();
    return STATUS_SUCCESS;
}

STDMETHODIMP_(NTSTATUS) CMiniportWaveRT::GetDeviceDescription(
    PDEVICE_DESCRIPTION DeviceDescription
)
{
    PAGED_CODE();
    UNREFERENCED_PARAMETER(DeviceDescription);
    return STATUS_NOT_SUPPORTED;
}
#pragma code_seg()

/* Factory */
#pragma code_seg("PAGE")
NTSTATUS CreateMiniportWaveRT(
    PUNKNOWN* Unknown, REFCLSID, PUNKNOWN UnknownOuter, POOL_TYPE
)
{
    PAGED_CODE();
    CMiniportWaveRT* p = new(NonPagedPoolNx, 'MrtA') CMiniportWaveRT(UnknownOuter);
    if (!p) return STATUS_INSUFFICIENT_RESOURCES;

    *Unknown = PUNKNOWN(PMINIPORTWAVERT(p));
    (*Unknown)->AddRef();
    return STATUS_SUCCESS;
}
#pragma code_seg()
