/*
 * toptable.h — Topology miniport declarations.
 */
#pragma once

#include "adapter.h"

class CMiniportTopology :
    public IMiniportTopology,
    public CUnknown
{
public:
    DECLARE_STD_UNKNOWN();

    CMiniportTopology(PUNKNOWN OuterUnknown) : CUnknown(OuterUnknown), m_Port(NULL) {}
    ~CMiniportTopology();

    IMP_IMiniportTopology;

private:
    PPORTTOPOLOGY m_Port;
};
