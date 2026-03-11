/*
 * shared.h — Shared memory layout and IOCTL codes for AirTune Virtual Audio Driver.
 * This is the contract between the kernel-mode WaveRT driver and the Go user-mode app.
 */
#pragma once

#ifdef _KERNEL_MODE
#include <ntddk.h>
typedef ULONG    uint32_t;
typedef USHORT   uint16_t;
typedef UCHAR    uint8_t;
typedef ULONG64  uint64_t;
typedef LONG64   int64_t;
#else
#include <stdint.h>
#endif

/* ── Shared memory section name (Global namespace for cross-session access) ── */
#define AIRTUNE_SHM_NAME       L"Global\\AirTuneAudioBuffer"
#define AIRTUNE_EVENT_NAME     L"Global\\AirTuneAudioDataReady"
#define AIRTUNE_DEVICE_NAME    L"\\Device\\AirTuneAudio"
#define AIRTUNE_SYMLINK_NAME   L"\\DosDevices\\Global\\AirTuneAudio"
#define AIRTUNE_USERMODE_PATH  L"\\\\.\\AirTuneAudio"

/* ── Magic / version ── */
#define AIRTUNE_SHM_MAGIC      0x41545642  /* 'ATVB' */
#define AIRTUNE_SHM_VERSION    1

/* ── Audio format (fixed — forces Windows SRC to resample) ── */
#define AIRTUNE_SAMPLE_RATE    44100
#define AIRTUNE_CHANNELS       2
#define AIRTUNE_BIT_DEPTH      16
#define AIRTUNE_BLOCK_ALIGN    (AIRTUNE_CHANNELS * (AIRTUNE_BIT_DEPTH / 8))  /* 4 */

/* ── Ring buffer size (256 KB — ~1.5s at 44100/16/2) ── */
#define AIRTUNE_RING_BUF_SIZE  (256 * 1024)

/* ── Shared memory header (64 bytes, followed by ring buffer) ── */
#pragma pack(push, 1)
typedef struct _AIRTUNE_SHM_HEADER {
    uint32_t Magic;          /* 0x41545642 */
    uint32_t Version;        /* 1 */
    uint32_t SampleRate;     /* 44100 */
    uint16_t Channels;       /* 2 */
    uint16_t BitDepth;       /* 16 */
    uint32_t BlockAlign;     /* 4 */
    uint32_t BufferSize;     /* 262144 */
    volatile int64_t  WriteOffset;    /* byte offset written by driver (monotonic mod BufferSize) */
    volatile int64_t  ReadOffset;     /* byte offset read by Go app (monotonic mod BufferSize) */
    volatile int64_t  FramesWritten;  /* monotonic frame counter */
    uint32_t Flags;          /* bit 0: stream active */
    uint8_t  Reserved[12];
    /* Ring buffer data follows at offset 64 */
} AIRTUNE_SHM_HEADER;
#pragma pack(pop)

#define AIRTUNE_SHM_HEADER_SIZE  64
#define AIRTUNE_SHM_TOTAL_SIZE   (AIRTUNE_SHM_HEADER_SIZE + AIRTUNE_RING_BUF_SIZE)

/* Flag bits */
#define AIRTUNE_FLAG_ACTIVE    0x00000001

/* ── IOCTL codes ── */
/* Using METHOD_BUFFERED, FILE_DEVICE_UNKNOWN, FILE_ANY_ACCESS */
#define FILE_DEVICE_AIRTUNE    0x00008000

#define IOCTL_AIRTUNE_SET_LATENCY  \
    CTL_CODE(FILE_DEVICE_AIRTUNE, 0x800, METHOD_BUFFERED, FILE_ANY_ACCESS)
    /* Input: uint64_t latency in 100-nanosecond units (HNS) */

#define IOCTL_AIRTUNE_GET_STATUS   \
    CTL_CODE(FILE_DEVICE_AIRTUNE, 0x801, METHOD_BUFFERED, FILE_ANY_ACCESS)
    /* Output: AIRTUNE_STATUS */

/* ── IOCTL payload structures ── */
typedef struct _AIRTUNE_STATUS {
    uint64_t FramesWritten;
    uint64_t LatencyHns;        /* current latency in 100ns units */
    uint32_t StreamActive;
    uint32_t Reserved;
} AIRTUNE_STATUS;
