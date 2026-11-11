//go:build linux

package fido

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const sysHidraw = "/sys/class/hidraw"

// System is the OS transport: hidraw. The device nodes are root-only
// unless a udev rule grants access (see docs/clients.md).
func System() (Transport, error) { return hidraw{}, nil }

type hidraw struct{}

// Devices lists the hidraw nodes whose report descriptor has the FIDO
// usage page. The ID is the HID device's sysfs name ("0003:1050:0407.000A"),
// whose last part the kernel never reuses while it runs.
func (hidraw) Devices() ([]DeviceInfo, error) {
	entries, err := os.ReadDir(sysHidraw)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var devices []DeviceInfo
	for _, entry := range entries {
		dir := filepath.Join(sysHidraw, entry.Name(), "device")
		desc, err := os.ReadFile(filepath.Join(dir, "report_descriptor"))
		if err != nil || !isFIDODescriptor(desc) {
			continue
		}
		id := entry.Name()
		if target, err := os.Readlink(dir); err == nil {
			id = filepath.Base(target)
		}
		devices = append(devices, DeviceInfo{
			ID:      id,
			Path:    "/dev/" + entry.Name(),
			Product: ueventValue(filepath.Join(dir, "uevent"), "HID_NAME"),
		})
	}
	return devices, nil
}

func ueventValue(path, key string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		if value, ok := strings.CutPrefix(scanner.Text(), key+"="); ok {
			return value
		}
	}
	return ""
}

func (hidraw) Open(info DeviceInfo) (Device, error) {
	fd, err := unix.Open(info.Path, unix.O_RDWR|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.EACCES) {
		return nil, fmt.Errorf("no access to %s: install the udev rule for security keys (see the NeoCloud docs)", info.Path)
	}
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", info.Path, err)
	}
	return &hidrawDevice{fd: fd}, nil
}

type hidrawDevice struct{ fd int }

// Write prepends report ID 0: FIDO devices use unnumbered reports.
func (d *hidrawDevice) Write(report []byte) error {
	buf := make([]byte, 1+reportSize)
	copy(buf[1:], report)
	n, err := unix.Write(d.fd, buf)
	if err != nil {
		return err
	}
	if n != len(buf) {
		return errors.New("fido: short write to the security key")
	}
	return nil
}

func (d *hidrawDevice) Read(timeout time.Duration) ([]byte, error) {
	fds := []unix.PollFd{{Fd: int32(d.fd), Events: unix.POLLIN}}
	n, err := unix.Poll(fds, int(timeout.Milliseconds()))
	if errors.Is(err, unix.EINTR) || (err == nil && n == 0) {
		return nil, ErrTimeout
	}
	if err != nil {
		return nil, err
	}
	if fds[0].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
		return nil, errors.New("fido: the security key was removed")
	}
	buf := make([]byte, reportSize)
	n, err = unix.Read(d.fd, buf)
	if err != nil {
		return nil, err
	}
	if n != reportSize {
		return nil, fmt.Errorf("fido: short HID report (%d bytes)", n)
	}
	return buf, nil
}

func (d *hidrawDevice) Close() error { return unix.Close(d.fd) }
