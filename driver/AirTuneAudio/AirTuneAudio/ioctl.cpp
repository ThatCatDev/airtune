/*
 * ioctl.cpp — IOCTL handler for AirTune Virtual Audio driver.
 */
#pragma warning(disable: 4996)

#include "adapter.h"

#pragma code_seg("PAGE")
NTSTATUS AirTuneIoctlDispatch(PDEVICE_OBJECT DeviceObject, PIRP Irp)
{
    PAGED_CODE();
    UNREFERENCED_PARAMETER(DeviceObject);

    PIO_STACK_LOCATION irpSp = IoGetCurrentIrpStackLocation(Irp);
    NTSTATUS status = STATUS_INVALID_DEVICE_REQUEST;
    ULONG_PTR info = 0;

    ULONG ioctl = irpSp->Parameters.DeviceIoControl.IoControlCode;
    PVOID buffer = Irp->AssociatedIrp.SystemBuffer;
    ULONG inLen  = irpSp->Parameters.DeviceIoControl.InputBufferLength;
    ULONG outLen = irpSp->Parameters.DeviceIoControl.OutputBufferLength;

    switch (ioctl) {
    case IOCTL_AIRTUNE_SET_LATENCY:
        if (inLen >= sizeof(LONG64) && buffer) {
            LONG64 latencyHns = *(LONG64*)buffer;
            InterlockedExchange64(&g_LatencyHns, latencyHns);
            status = STATUS_SUCCESS;
        } else {
            status = STATUS_BUFFER_TOO_SMALL;
        }
        break;

    case IOCTL_AIRTUNE_GET_STATUS:
        if (outLen >= sizeof(AIRTUNE_STATUS) && buffer) {
            AIRTUNE_STATUS* st = (AIRTUNE_STATUS*)buffer;
            st->FramesWritten = (uint64_t)InterlockedCompareExchange64(&g_FramesWritten, 0, 0);
            st->LatencyHns    = (uint64_t)InterlockedCompareExchange64(&g_LatencyHns, 0, 0);
            st->StreamActive  = (g_SharedMemory != NULL) ?
                ((AIRTUNE_SHM_HEADER*)g_SharedMemory)->Flags & AIRTUNE_FLAG_ACTIVE : 0;
            st->Reserved = 0;
            info = sizeof(AIRTUNE_STATUS);
            status = STATUS_SUCCESS;
        } else {
            status = STATUS_BUFFER_TOO_SMALL;
        }
        break;

    default:
        status = STATUS_INVALID_DEVICE_REQUEST;
        break;
    }

    Irp->IoStatus.Status = status;
    Irp->IoStatus.Information = info;
    IoCompleteRequest(Irp, IO_NO_INCREMENT);
    return status;
}
#pragma code_seg()
