// Copyright (c) 2017, NVIDIA CORPORATION. All rights reserved.

package main

import (
	"log"
	"strconv"
	"strings"

	"github.com/NVIDIA/gpu-monitoring-tools/bindings/go/nvml"

	"golang.org/x/net/context"
	pluginapi "k8s.io/kubernetes/pkg/kubelet/apis/deviceplugin/v1beta1"
)

const nGPU uint = 4

func check(err error) {
	if err != nil {
		log.Panicln("Fatal:", err)
	}
}

func getDevices() []*pluginapi.Device {
	n, err := nvml.GetDeviceCount()
	check(err)

	var devs []*pluginapi.Device
	for i := uint(0); i < n; i++ {
		d, err := nvml.NewDeviceLite(i)
		check(err)
		devs = append(devs, &pluginapi.Device{
			ID:     d.UUID,
			Health: pluginapi.Healthy,
		})
	}

	return devs
}

func getVirutalDevices() []*pluginapi.Device {
	n, err := nvml.GetDeviceCount()
	check(err)

	var vdevs []*pluginapi.Device
	for i := uint(0); i < n; i++ {
		d, err := nvml.NewDevice(i)
		check(err)
		for j := uint(0); j < nGPU; j++ {
			vdevs = append(vdevs, &pluginapi.Device{
				ID:     d.UUID + "-" + strconv.Itoa(int(j)),
				Health: pluginapi.Healthy,
			})
			log.Printf("append %s as virtual of %s into virtual_dev", d.UUID+"-"+strconv.Itoa(int(j)), d.UUID)
		}
	}

	return vdevs
}

func deviceExists(devs []*pluginapi.Device, id string) bool {
	for _, d := range devs {
		if d.ID == id {
			return true
		}
	}
	return false
}

func watchXIDs(ctx context.Context, devs []*pluginapi.Device, xids chan<- *pluginapi.Device) {
	eventSet := nvml.NewEventSet()
	defer nvml.DeleteEventSet(eventSet)

	// perhaps introduce unknown error

	for _, d := range devs {
		err := nvml.RegisterEventForDevice(eventSet, nvml.XidCriticalError, GetTrueID(d.ID))
		if err != nil && strings.HasSuffix(err.Error(), "Not Supported") {
			log.Printf("Warning: %s is too old to support healthchecking: %s. Marking it unhealthy.", d.ID, err)

			xids <- d
			continue
		}

		if err != nil {
			log.Panicln("Fatal:", err)
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		e, err := nvml.WaitForEvent(eventSet, 5000)
		if err != nil && e.Etype != nvml.XidCriticalError {
			continue
		}

		// FIXME: formalize the full list and document it.
		// http://docs.nvidia.com/deploy/xid-errors/index.html#topic_4
		// Application errors: the GPU should still be healthy
		if e.Edata == 31 || e.Edata == 43 || e.Edata == 45 {
			continue
		}

		if e.UUID == nil || len(*e.UUID) == 0 {
			// All devices are unhealthy
			for _, d := range devs {
				xids <- d
			}
			continue
		}

		for _, d := range devs {
			if d.ID == *e.UUID {
				xids <- d
			}
		}
	}
}
