// Copyright 2013 Google Inc.  All rights reserved.
// Copyright 2016 the gousb Authors.  All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

/*
Package gousb provides an low-level interface to attached USB devices.

A Short Tutorial

A Context manages all resources necessary for communicating with USB
devices.
Through the Context users can iterate over available USB devices,

The USB standard defines a mechanism of discovering USB device functionality
through a mechanism of descriptors. After the device is attached and
initialized by the host stack, it's possible to retrieve it's descriptor
(the device descriptor). It contains elements such as product and vendor IDs,
bus number and device number (address) on the bus.

In gousb Device struct represents the USB device, and Device.Desc
contains all the information known about the device.

Among other information in the device descriptor is a list of configuration
descriptors, accessible through Device.Descriptor.Configs.

USB standard allows one physical USB device to switch between different
sets of behaviors, or working modes, by selecting one of the offered configs
(each device has at least one).
This allows the same device to sometimes present itself as e.g. a 3G modem,
and sometimes a flash drive. Configs are mutually exclusive, each device
can have only one active config at a time. Switching the active config performs
a light-weight device reset. Each config in the device descriptor has
a unique identification number.

In gousb a device config needs to be selected through Device.Config(num).
It returns a Config struct that represents the device in this particular configuration.
The configuration descriptor is accessible through Config.Info.

A config descriptor determines the list of available USB interfaces on the device.
Each interface is a virtual device within the physical USB device and it's active
config. There can be many interfaces active concurrently. Interfaces are
enumerated sequentially starting from zero.

Additionally, each interface comes with a number of alternate settings for
the interface, which are somewhat similar to device configs, but on the
interface level. Each interface can have only a single alternate setting
active at any time. Alternate settings are enumerated sequentially starting from
zero.

In gousb an interface and it's alternate setting can be selected through
Config.Interface(num, altNum). The Interface struct is the representation
of the claimed interface with a particular alternate setting.
The descriptor of the interface is available through Interface.Setting.

An interface with a particular alternate setting defines up to 15
endpoints. An endpoint can be considered similar to a UDP/IP port,
except the data transfers are unidirectional.

Endpoints are represented by the Endpoint struct, and all defined endpoints
can be obtained through the Endpoints field of the Interface.Setting.

Each endpoint descriptor (EndpointInfo) defined in the interface's endpoint
map includes information about the type of the endpoint:

- endpoint number

- direction: IN (device-to-host) or OUT (host-to-device)

- transfer type: USB standard defines a few distinct data transfer types:

--- bulk - high throughput, but no guaranteed bandwidth and no latency guarantees,

--- isochronous - medium throughput, guaranteed bandwidth, some latency guarantees,

--- interrupt - low throughput, high latency guarantees.

The endpoint descriptor determines the type of the transfer that will be used.

- maximum packet size: maximum number of bytes that can be sent or received by the device in a single USB transaction.
and a few other less frequently used pieces of endpoint information.

An IN Endpoint can be opened for reading through Interface.InEndpoint(epNum),
while an OUT Endpoint can be opened for writing through Interface.OutEndpoint(epNum).

An InEndpoint implements the io.Reader interface, an OutEndpoint implements
the io.Writer interface. Both Reads and Writes will accept larger slices
of data than the endpoint's maximum packet size, the transfer will be split
into smaller USB transactions as needed. But using Read/Write size equal
to an integer multiple of maximum packet size helps with improving the transfer
performance.

Apart from 15 possible data endpoints, each USB device also has a control endpoint.
The control endpoint is present regardless of the current device config, claimed
interfaces and their alternate settings. It makes a lot of sense, as the control endpoint is actually used, among others,
to issue commands to switch the active config or select an alternate setting for an interface.

Control commands are also ofen use to control the behavior of the device. There is no single
standard for control commands though, and many devices implement their custom control command schema.

Control commands can be issued through Device.Control().

See Also

For more information about USB protocol and handling USB devices,
see the excellent "USB in a nutshell" guide: http://www.beyondlogic.org/usbnutshell/

*/
package gousb

// Context manages all resources related to USB device handling.
type Context struct {
	ctx  *libusbContext
	done chan struct{}
}

// Debug changes the debug level. Level 0 means no debug, higher levels
// will print out more debugging information.
func (c *Context) Debug(level int) {
	libusb.setDebug(c.ctx, level)
}

// NewContext returns a new Context instance.
func NewContext() *Context {
	c, err := libusb.init()
	if err != nil {
		panic(err)
	}
	ctx := &Context{
		ctx:  c,
		done: make(chan struct{}),
	}
	go libusb.handleEvents(ctx.ctx, ctx.done)
	return ctx
}

// ListDevices calls each with each enumerated device.
// If the function returns true, the device is opened and a Device is returned if the operation succeeds.
// Every Device returned (whether an error is also returned or not) must be closed.
// If there are any errors enumerating the devices,
// the final one is returned along with any successfully opened devices.
func (c *Context) ListDevices(each func(desc *DeviceDesc) bool) ([]*Device, error) {
	list, err := libusb.getDevices(c.ctx)
	if err != nil {
		return nil, err
	}

	var reterr error
	var ret []*Device
	for _, dev := range list {
		desc, err := libusb.getDeviceDesc(dev)
		if err != nil {
			libusb.dereference(dev)
			reterr = err
			continue
		}

		if each(desc) {
			handle, err := libusb.open(dev)
			if err != nil {
				reterr = err
				continue
			}
			ret = append(ret, &Device{handle: handle, Desc: desc})
		} else {
			libusb.dereference(dev)
		}
	}
	return ret, reterr
}

// OpenDeviceWithVIDPID opens Device from specific VendorId and ProductId.
// If none is found, it returns nil and nil error. If there are multiple devices
// with the same VID/PID, it will return one of them, picked arbitrarily.
// If there were any errors during device list traversal, it is possible
// it will return a non-nil device and non-nil error. A Device.Close() must
// be called to release the device if the returned device wasn't nil.
func (c *Context) OpenDeviceWithVIDPID(vid, pid ID) (*Device, error) {
	var found bool
	devs, err := c.ListDevices(func(desc *DeviceDesc) bool {
		if found {
			return false
		}
		if desc.Vendor == ID(vid) && desc.Product == ID(pid) {
			found = true
			return true
		}
		return false
	})
	if len(devs) == 0 {
		return nil, err
	}
	return devs[0], nil
}

// Close releases the Context and all associated resources.
func (c *Context) Close() error {
	c.done <- struct{}{}
	if c.ctx != nil {
		libusb.exit(c.ctx)
	}
	c.ctx = nil
	return nil
}
