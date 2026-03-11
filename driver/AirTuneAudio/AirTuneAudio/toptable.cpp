/*
 * toptable.cpp — Minimal topology miniport for AirTune Virtual Audio.
 */
#pragma warning(disable: 4996)

#include "toptable.h"
#include "shared.h"

/* ── Audio format (must match wave miniport) ── */

static KSDATARANGE_AUDIO g_TopoDataRange = {
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

static PKSDATARANGE g_TopoDataRangePointers[] = {
    PKSDATARANGE(&g_TopoDataRange)
};

/* ── Topology descriptors ── */

static PCPIN_DESCRIPTOR g_TopoPinDescriptors[] = {
    /* Pin 0: From wave (input) */
    {
        0, 0, 0,
        NULL,
        {
            0, NULL,
            0, NULL,
            SIZEOF_ARRAY(g_TopoDataRangePointers),
            g_TopoDataRangePointers,
            KSPIN_DATAFLOW_IN,
            KSPIN_COMMUNICATION_NONE,
            &KSCATEGORY_AUDIO,
            NULL,
            { 0 }
        }
    },
    /* Pin 1: To speaker (output) */
    {
        0, 0, 0,
        NULL,
        {
            0, NULL,
            0, NULL,
            SIZEOF_ARRAY(g_TopoDataRangePointers),
            g_TopoDataRangePointers,
            KSPIN_DATAFLOW_OUT,
            KSPIN_COMMUNICATION_NONE,
            &KSNODETYPE_SPEAKER,
            NULL,
            { 0 }
        }
    }
};

/* No nodes — pure passthrough */
static PCCONNECTION_DESCRIPTOR g_TopoConnections[] = {
    { PCFILTER_NODE, 0,  PCFILTER_NODE, 1 }
};

static PCFILTER_DESCRIPTOR g_TopoFilterDescriptor = {
    0, NULL,
    sizeof(PCPIN_DESCRIPTOR),
    SIZEOF_ARRAY(g_TopoPinDescriptors),
    g_TopoPinDescriptors,
    0, 0, NULL,    /* no nodes */
    SIZEOF_ARRAY(g_TopoConnections),
    g_TopoConnections,
    0, NULL
};

/* ── CMiniportTopology ── */

CMiniportTopology::~CMiniportTopology()
{
    if (m_Port) { m_Port->Release(); m_Port = NULL; }
}

STDMETHODIMP_(NTSTATUS) CMiniportTopology::NonDelegatingQueryInterface(
    REFIID Interface, PVOID* Object
)
{
    PAGED_CODE();
    ASSERT(Object);

    if (IsEqualGUIDAligned(Interface, IID_IUnknown))
        *Object = PVOID(PUNKNOWN(PMINIPORTTOPOLOGY(this)));
    else if (IsEqualGUIDAligned(Interface, IID_IMiniport))
        *Object = PVOID(PMINIPORT(this));
    else if (IsEqualGUIDAligned(Interface, IID_IMiniportTopology))
        *Object = PVOID(PMINIPORTTOPOLOGY(this));
    else {
        *Object = NULL;
        return STATUS_INVALID_PARAMETER;
    }

    PUNKNOWN(*Object)->AddRef();
    return STATUS_SUCCESS;
}

#pragma code_seg("PAGE")
STDMETHODIMP_(NTSTATUS) CMiniportTopology::Init(
    PUNKNOWN UnknownAdapter, PRESOURCELIST ResourceList, PPORTTOPOLOGY Port
)
{
    PAGED_CODE();
    UNREFERENCED_PARAMETER(UnknownAdapter);
    UNREFERENCED_PARAMETER(ResourceList);
    m_Port = Port;
    m_Port->AddRef();
    return STATUS_SUCCESS;
}

STDMETHODIMP_(NTSTATUS) CMiniportTopology::GetDescription(
    PPCFILTER_DESCRIPTOR* Description
)
{
    PAGED_CODE();
    *Description = &g_TopoFilterDescriptor;
    return STATUS_SUCCESS;
}

STDMETHODIMP_(NTSTATUS) CMiniportTopology::DataRangeIntersection(
    ULONG PinId, PKSDATARANGE DataRange, PKSDATARANGE MatchingDataRange,
    ULONG OutputBufferLength, PVOID ResultantFormat, PULONG ResultantFormatLength
)
{
    PAGED_CODE();
    UNREFERENCED_PARAMETER(PinId);
    UNREFERENCED_PARAMETER(DataRange);
    UNREFERENCED_PARAMETER(MatchingDataRange);
    UNREFERENCED_PARAMETER(OutputBufferLength);
    UNREFERENCED_PARAMETER(ResultantFormat);
    UNREFERENCED_PARAMETER(ResultantFormatLength);
    return STATUS_NOT_IMPLEMENTED;
}
#pragma code_seg()

/* Factory */
#pragma code_seg("PAGE")
NTSTATUS CreateMiniportTopology(
    PUNKNOWN* Unknown, REFCLSID, PUNKNOWN UnknownOuter, POOL_TYPE
)
{
    PAGED_CODE();
    CMiniportTopology* p = new(NonPagedPoolNx, 'TrtA') CMiniportTopology(UnknownOuter);
    if (!p) return STATUS_INSUFFICIENT_RESOURCES;

    *Unknown = PUNKNOWN(PMINIPORTTOPOLOGY(p));
    (*Unknown)->AddRef();
    return STATUS_SUCCESS;
}
#pragma code_seg()
