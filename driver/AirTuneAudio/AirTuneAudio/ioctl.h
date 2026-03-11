/*
 * ioctl.h — IOCTL handler declarations.
 */
#pragma once

#include <ntddk.h>

NTSTATUS AirTuneIoctlDispatch(PDEVICE_OBJECT DeviceObject, PIRP Irp);
