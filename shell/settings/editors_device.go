package settings

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/pulse"
	"github.com/stubbedev/wayle/service/recorder"
)

// deviceSelect is DeviceSelectControl: a dropdown of devices writing
// the picked id, a saved device the list lacks kept selectable.
type deviceSelect struct {
	*widget.Dropdown
	slot    slot
	choices []recorder.DeviceChoice
	syncing bool
}

func newDeviceSelect(k *kit, s slot, choices []recorder.DeviceChoice) *deviceSelect {
	c := &deviceSelect{Dropdown: widget.NewDropdown(k.face, 14, nil, 0), slot: s}
	c.OnSelect = func(i int) {
		if !c.syncing && i >= 0 && i < len(c.choices) {
			_ = c.slot.set(c.choices[i].ID)
		}
	}
	c.setChoices(choices)
	return c
}

// setChoices replaces the list (SetChoices), selecting the saved id.
func (c *deviceSelect) setChoices(choices []recorder.DeviceChoice) {
	saved := c.slot.text()
	c.choices = recorder.WithSaved(choices, saved)
	c.SetItems(recorder.ChoiceLabels(c.choices), recorder.ChoiceIndex(c.choices, saved))
}

// refresh selects the saved id without writing.
func (c *deviceSelect) refresh() {
	c.syncing = true
	c.SetSelected(recorder.ChoiceIndex(c.choices, c.slot.text()))
	c.syncing = false
}

// cameraChoices lists the cameras (a seam for tests).
var cameraChoices = recorder.Cameras

// microphoneQueryTimeout bounds the audio daemon's source listing.
const microphoneQueryTimeout = 5 * time.Second

// microphoneChoices asks the audio daemon for its sources
// (com.wayle.Audio1 ListSources), monitors dropped by their name; with
// no daemon the list is the default alone (a seam for tests).
var microphoneChoices = func() []recorder.DeviceChoice {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return recorder.MicrophoneChoices(nil)
	}
	defer func() { _ = conn.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), microphoneQueryTimeout)
	defer cancel()
	entries, err := pulse.NewClient(conn).ListSources(ctx)
	if err != nil {
		log.Printf("settings: microphones: %v", err)
		return recorder.MicrophoneChoices(nil)
	}
	sources := make([]recorder.Source, len(entries))
	for i, e := range entries {
		sources[i] = recorder.Source{Name: e.Name, Description: e.Description, Monitor: strings.HasSuffix(e.Name, ".monitor")}
	}
	return recorder.MicrophoneChoices(sources)
}

// webcamDevice is webcam_device_select.
var webcamDevice = withEditor(func(k *kit, s slot, _ config.FieldMeta) control {
	return newDeviceSelect(k, s, cameraChoices())
})

// microphoneDevice is microphone_device_select: the default alone at
// first, the daemon's sources once they arrive.
var microphoneDevice = withEditor(func(k *kit, s slot, _ config.FieldMeta) control {
	c := newDeviceSelect(k, s, recorder.MicrophoneChoices(nil))
	async(k, microphoneChoices, c.setChoices)
	return c
})
