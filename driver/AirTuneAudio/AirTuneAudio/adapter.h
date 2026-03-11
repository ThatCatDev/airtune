/*
 * adapter.h — AirTune Virtual Audio Driver adapter declarations.
 */
#pragma once

#include <ntddk.h>

#pragma warning(push)
#pragma warning(disable: 4996)  /* ExAllocatePoolWithTag deprecated in stdunk.h */
#include <portcls.h>
#include <stdunk.h>
#pragma warning(pop)

#include "shared.h"

/* Forward declarations */
NTSTATUS CreateMiniportWaveRT(PUNKNOWN* Unknown, REFCLSID, PUNKNOWN UnknownOuter, POOL_TYPE PoolType);
NTSTATUS CreateMiniportTopology(PUNKNOWN* Unknown, REFCLSID, PUNKNOWN UnknownOuter, POOL_TYPE PoolType);

/* IOCTL dispatch */
NTSTATUS AirTuneIoctlDispatch(PDEVICE_OBJECT DeviceObject, PIRP Irp);

/* Globals */
extern volatile LONG64  g_LatencyHns;
extern volatile LONG64  g_FramesWritten;
extern PVOID            g_SharedMemory;
extern PKEVENT          g_DataReadyEvent;

/* Diagnostics */
void DiagWrite(ULONG step, NTSTATUS status);
void DiagWrite64(ULONG step, LONG64 value);
