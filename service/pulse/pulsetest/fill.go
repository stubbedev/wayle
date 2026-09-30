package pulsetest

import "github.com/stubbedev/wayle/service/pulse/native"

// The fill functions are the server half of the wire layouts, written
// from protocol-native.c (sink_fill_tagstruct and friends) with its
// version gates, independently of the client's decoders so a layout
// mistake on either side shows up as a test failure.

func fillDevice(w *native.Writer, v uint32, d native.DeviceInfo, isSink bool) {
	w.U32(d.Index)
	w.String(d.Name)
	w.OptString(d.Description)
	w.SampleSpec(d.SampleSpec)
	w.ChannelMap(d.ChannelMap)
	w.U32(d.OwnerModule)
	w.CVolume(d.Volume)
	w.Bool(d.Mute)
	w.U32(d.Monitor)
	w.OptString(d.MonitorName)
	w.Usec(d.Latency)
	w.OptString(d.Driver)
	w.U32(d.Flags)
	if v >= 13 {
		w.PropList(d.Props)
		w.Usec(d.ConfiguredLatency)
	}
	if v >= 15 {
		w.Volume(d.BaseVolume)
		w.U32(d.State)
		w.U32(d.VolumeSteps)
		w.U32(d.Card)
	}
	if v >= 16 {
		w.U32(uint32(len(d.Ports)))
		for _, p := range d.Ports {
			w.String(p.Name)
			w.OptString(p.Description)
			w.U32(p.Priority)
			if v >= 24 {
				w.U32(p.Available)
				if v >= 34 {
					w.NullString() // availability_group
					w.U32(0)       // type
				}
			}
		}
		w.OptString(d.ActivePort)
	}
	formatsVersion := uint32(22)
	if isSink {
		formatsVersion = 21
	}
	if v >= formatsVersion {
		w.U8(uint8(len(d.Formats)))
		for _, f := range d.Formats {
			w.FormatInfo(f)
		}
	}
}

func fillSinkInput(w *native.Writer, v uint32, s native.StreamInfo) {
	w.U32(s.Index)
	w.OptString(s.Name)
	w.U32(s.OwnerModule)
	w.U32(s.Client)
	w.U32(s.Device)
	w.SampleSpec(s.SampleSpec)
	w.ChannelMap(s.ChannelMap)
	w.CVolume(s.Volume)
	w.Usec(s.BufferLatency)
	w.Usec(s.DeviceLatency)
	w.OptString(s.ResampleMethod)
	w.OptString(s.Driver)
	if v >= 11 {
		w.Bool(s.Mute)
	}
	if v >= 13 {
		w.PropList(s.Props)
	}
	if v >= 19 {
		w.Bool(s.Corked)
	}
	if v >= 20 {
		w.Bool(s.HasVolume)
		w.Bool(s.VolumeWritable)
	}
	if v >= 21 {
		w.FormatInfo(s.Format)
	}
}

func fillSourceOutput(w *native.Writer, v uint32, s native.StreamInfo) {
	w.U32(s.Index)
	w.OptString(s.Name)
	w.U32(s.OwnerModule)
	w.U32(s.Client)
	w.U32(s.Device)
	w.SampleSpec(s.SampleSpec)
	w.ChannelMap(s.ChannelMap)
	w.Usec(s.BufferLatency)
	w.Usec(s.DeviceLatency)
	w.OptString(s.ResampleMethod)
	w.OptString(s.Driver)
	if v >= 13 {
		w.PropList(s.Props)
	}
	if v >= 19 {
		w.Bool(s.Corked)
	}
	if v >= 22 {
		w.CVolume(s.Volume)
		w.Bool(s.Mute)
		w.Bool(s.HasVolume)
		w.Bool(s.VolumeWritable)
		w.FormatInfo(s.Format)
	}
}

func fillServerInfo(w *native.Writer, v uint32, info native.ServerInfo) {
	w.OptString(info.PackageName)
	w.OptString(info.PackageVersion)
	w.OptString(info.UserName)
	w.OptString(info.HostName)
	w.SampleSpec(info.SampleSpec)
	w.OptString(info.DefaultSink)
	w.OptString(info.DefaultSource)
	w.U32(info.Cookie)
	if v >= 15 {
		w.ChannelMap(info.ChannelMap)
	}
}
