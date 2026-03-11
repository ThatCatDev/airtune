/*
 * guids.cpp — Instantiate GUIDs used by the driver.
 * INITGUID must be defined before including headers in exactly one translation unit.
 */
#pragma warning(disable: 4996)

#define INITGUID

#include <ntddk.h>
#include <portcls.h>
#include <ksmedia.h>
