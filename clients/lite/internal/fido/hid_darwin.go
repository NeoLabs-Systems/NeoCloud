//go:build darwin && cgo

package fido

/*
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation
#include <CoreFoundation/CoreFoundation.h>
#include <IOKit/IOKitLib.h>
#include <IOKit/hid/IOHIDDevice.h>
#include <IOKit/hid/IOHIDKeys.h>
#include <IOKit/hid/IOHIDManager.h>
#include <stdlib.h>
#include <string.h>

typedef struct {
	uint64_t id;
	char product[128];
} nc_info;

static void nc_set_int(CFMutableDictionaryRef dict, CFStringRef key, int value) {
	CFNumberRef number = CFNumberCreate(kCFAllocatorDefault, kCFNumberIntType, &value);
	CFDictionarySetValue(dict, key, number);
	CFRelease(number);
}

// nc_list writes up to max FIDO HID devices (usage page 0xF1D0, usage 1)
// to out and returns how many, or -1.
static int nc_list(nc_info *out, int max) {
	IOHIDManagerRef manager = IOHIDManagerCreate(kCFAllocatorDefault, kIOHIDOptionsTypeNone);
	if (manager == NULL) return -1;
	CFMutableDictionaryRef match = CFDictionaryCreateMutable(kCFAllocatorDefault, 0,
		&kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	nc_set_int(match, CFSTR(kIOHIDDeviceUsagePageKey), 0xf1d0);
	nc_set_int(match, CFSTR(kIOHIDDeviceUsageKey), 0x01);
	IOHIDManagerSetDeviceMatching(manager, match);
	CFRelease(match);
	int n = 0;
	CFSetRef set = IOHIDManagerCopyDevices(manager);
	if (set != NULL) {
		CFIndex count = CFSetGetCount(set);
		IOHIDDeviceRef *devices = calloc(count > 0 ? count : 1, sizeof(IOHIDDeviceRef));
		CFSetGetValues(set, (const void **)devices);
		for (CFIndex i = 0; i < count && n < max; i++) {
			io_service_t service = IOHIDDeviceGetService(devices[i]);
			uint64_t id;
			if (service == MACH_PORT_NULL || IORegistryEntryGetRegistryEntryID(service, &id) != KERN_SUCCESS) continue;
			out[n].id = id;
			out[n].product[0] = 0;
			CFTypeRef product = IOHIDDeviceGetProperty(devices[i], CFSTR(kIOHIDProductKey));
			if (product != NULL && CFGetTypeID(product) == CFStringGetTypeID()) {
				CFStringGetCString((CFStringRef)product, out[n].product, sizeof(out[n].product), kCFStringEncodingUTF8);
			}
			n++;
		}
		free(devices);
		CFRelease(set);
	}
	CFRelease(manager);
	return n;
}

#define NC_QUEUE 32

typedef struct {
	IOHIDDeviceRef device;
	CFStringRef mode;
	uint8_t inbuf[64];
	uint8_t queue[NC_QUEUE][64];
	int head, count, gone;
} nc_dev;

static void nc_on_report(void *ctx, IOReturn result, void *sender, IOHIDReportType type,
		uint32_t id, uint8_t *report, CFIndex len) {
	nc_dev *d = ctx;
	if (result != kIOReturnSuccess || len != 64 || d->count == NC_QUEUE) return;
	memcpy(d->queue[(d->head + d->count) % NC_QUEUE], report, 64);
	d->count++;
}

static void nc_on_removed(void *ctx, IOReturn result, void *sender) {
	((nc_dev *)ctx)->gone = 1;
}

// nc_open opens the device with the registry entry id. Reports arrive
// through the calling thread's run loop, in a private mode.
static nc_dev *nc_open(uint64_t id, int *status) {
	io_service_t service = IOServiceGetMatchingService(MACH_PORT_NULL, IORegistryEntryIDMatching(id));
	if (service == MACH_PORT_NULL) { *status = -1; return NULL; }
	IOHIDDeviceRef device = IOHIDDeviceCreate(kCFAllocatorDefault, service);
	IOObjectRelease(service);
	if (device == NULL) { *status = -1; return NULL; }
	IOReturn result = IOHIDDeviceOpen(device, kIOHIDOptionsTypeNone);
	if (result != kIOReturnSuccess) { CFRelease(device); *status = result; return NULL; }
	nc_dev *d = calloc(1, sizeof(nc_dev));
	d->device = device;
	d->mode = CFStringCreateWithFormat(kCFAllocatorDefault, NULL, CFSTR("neocloud-lite-fido-%p"), (void *)d);
	IOHIDDeviceScheduleWithRunLoop(device, CFRunLoopGetCurrent(), d->mode);
	IOHIDDeviceRegisterInputReportCallback(device, d->inbuf, sizeof(d->inbuf), nc_on_report, d);
	IOHIDDeviceRegisterRemovalCallback(device, nc_on_removed, d);
	*status = 0;
	return d;
}

static int nc_write(nc_dev *d, const uint8_t *report) {
	return IOHIDDeviceSetReport(d->device, kIOHIDReportTypeOutput, 0, report, 64) == kIOReturnSuccess ? 0 : -1;
}

// nc_read waits up to ms for a report: 1 when out holds one, 0 on
// timeout, -1 when the device is gone.
static int nc_read(nc_dev *d, uint8_t *out, int ms) {
	CFAbsoluteTime end = CFAbsoluteTimeGetCurrent() + ms / 1000.0;
	while (d->count == 0 && !d->gone) {
		CFTimeInterval left = end - CFAbsoluteTimeGetCurrent();
		if (left <= 0) return 0;
		if (CFRunLoopRunInMode(d->mode, left, true) == kCFRunLoopRunFinished) return -1;
	}
	if (d->count == 0) return -1;
	memcpy(out, d->queue[d->head], 64);
	d->head = (d->head + 1) % NC_QUEUE;
	d->count--;
	return 1;
}

static void nc_close(nc_dev *d) {
	IOHIDDeviceRegisterInputReportCallback(d->device, d->inbuf, sizeof(d->inbuf), NULL, NULL);
	IOHIDDeviceRegisterRemovalCallback(d->device, NULL, NULL);
	IOHIDDeviceUnscheduleFromRunLoop(d->device, CFRunLoopGetCurrent(), d->mode);
	IOHIDDeviceClose(d->device, kIOHIDOptionsTypeNone);
	CFRelease(d->device);
	CFRelease(d->mode);
	free(d);
}
*/
import "C"

import (
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"time"
	"unsafe"
)

const maxDevices = 16

// System is the OS transport: IOKit HID.
func System() (Transport, error) { return iokit{}, nil }

type iokit struct{}

// Devices lists FIDO HID devices. The ID is the IORegistry entry id,
// which macOS never reuses while it runs.
func (iokit) Devices() ([]DeviceInfo, error) {
	var infos [maxDevices]C.nc_info
	n := int(C.nc_list(&infos[0], maxDevices))
	if n < 0 {
		return nil, errors.New("fido: could not list HID devices")
	}
	devices := make([]DeviceInfo, 0, n)
	for i := 0; i < n; i++ {
		id := strconv.FormatUint(uint64(infos[i].id), 10)
		devices = append(devices, DeviceInfo{
			ID:      id,
			Path:    "IOService:" + id,
			Product: C.GoString(&infos[i].product[0]),
		})
	}
	return devices, nil
}

// Open binds the calling goroutine to its OS thread until Close: input
// reports are delivered through that thread's run loop.
func (iokit) Open(info DeviceInfo) (Device, error) {
	id, err := strconv.ParseUint(info.ID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("fido: bad device id %q", info.ID)
	}
	runtime.LockOSThread()
	var status C.int
	dev := C.nc_open(C.uint64_t(id), &status)
	if dev == nil {
		runtime.UnlockOSThread()
		return nil, fmt.Errorf("fido: could not open the security key (IOKit 0x%x)", uint32(status))
	}
	return &iokitDevice{dev: dev}, nil
}

type iokitDevice struct{ dev *C.nc_dev }

func (d *iokitDevice) Write(report []byte) error {
	if len(report) != reportSize {
		return errors.New("fido: reports are 64 bytes")
	}
	if C.nc_write(d.dev, (*C.uint8_t)(unsafe.Pointer(&report[0]))) != 0 {
		return errors.New("fido: writing to the security key failed")
	}
	return nil
}

func (d *iokitDevice) Read(timeout time.Duration) ([]byte, error) {
	buf := make([]byte, reportSize)
	switch C.nc_read(d.dev, (*C.uint8_t)(unsafe.Pointer(&buf[0])), C.int(timeout.Milliseconds())) {
	case 1:
		return buf, nil
	case 0:
		return nil, ErrTimeout
	}
	return nil, errors.New("fido: the security key was removed")
}

func (d *iokitDevice) Close() error {
	C.nc_close(d.dev)
	runtime.UnlockOSThread()
	return nil
}
