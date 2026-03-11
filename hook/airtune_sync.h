/*
 * airtune_sync.h — Shared memory layout for AirTune A/V sync hook.
 * Contract between the Go app and the injected DLL.
 */
#pragma once

#include <cstdint>

#define AIRTUNE_SYNC_SHM_NAME   L"Local\\AirTuneSyncLatency"
#define AIRTUNE_SYNC_MAGIC      0x41545359  /* 'ATSY' */
#define AIRTUNE_SYNC_VERSION    1
#define AIRTUNE_SYNC_SHM_SIZE   64

#pragma pack(push, 1)
struct AirTuneSyncData {
    uint32_t         magic;         /* 0x41545359 */
    uint32_t         version;       /* 1 */
    volatile int64_t latency_hns;   /* latency in 100ns units */
    volatile uint32_t sample_rate;  /* e.g. 44100 */
    volatile uint32_t enabled;      /* 0 = passthrough, 1 = active */
    uint8_t          reserved[40];
};
#pragma pack(pop)
