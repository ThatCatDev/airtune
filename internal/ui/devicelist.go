//go:build cgo

package ui

import (
	"log"
	"strings"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"airtune/internal/audio"
	"airtune/internal/discovery"
	"airtune/internal/raop"
	"airtune/internal/service"
)

// friendlyModel returns a human-readable name for the AirPlay device model identifier.
func friendlyModel(model string) string {
	m := strings.ToLower(model)
	switch {
	case strings.HasPrefix(m, "audioaccessory6"):
		return "HomePod mini"
	case strings.HasPrefix(m, "audioaccessory5"):
		return "HomePod (2nd gen)"
	case strings.HasPrefix(m, "audioaccessory1"):
		return "HomePod"
	case strings.HasPrefix(m, "audioaccessory"):
		return "HomePod"
	case strings.HasPrefix(m, "appletv"):
		return "Apple TV"
	case strings.HasPrefix(m, "airportexpress"), strings.HasPrefix(m, "airport"):
		return "AirPort Express"
	case strings.HasPrefix(m, "macbookpro"):
		return "MacBook Pro"
	case strings.HasPrefix(m, "macbookair"):
		return "MacBook Air"
	case strings.HasPrefix(m, "macbook"):
		return "MacBook"
	case strings.HasPrefix(m, "macmini"):
		return "Mac mini"
	case strings.HasPrefix(m, "macpro"):
		return "Mac Pro"
	case strings.HasPrefix(m, "imac"):
		return "iMac"
	case strings.HasPrefix(m, "mac"):
		return "Mac"
	case strings.HasPrefix(m, "iphone"):
		return "iPhone"
	case strings.HasPrefix(m, "ipad"):
		return "iPad"
	case strings.HasPrefix(m, "ipod"):
		return "iPod"
	default:
		return model
	}
}

// DeviceList manages the GTK ListBox of discovered AirPlay devices.
type DeviceList struct {
	ListBox     *gtk.ListBox
	manager     *service.Manager
	rows        map[string]*deviceRow   // rowID → row (device ID or "pair-"+gid)
	deviceToRow map[string]string       // deviceID → pair rowID (for routing session state)
}

type deviceRow struct {
	row       *gtk.ListBoxRow
	nameLabel *gtk.Label
	infoLabel *gtk.Label
	button    *gtk.Button
	chanDrop  *gtk.DropDown // nil for pair rows
	icon      *gtk.Image
	device    discovery.AirPlayDevice
	connected bool

	// Stereo pair fields (only set for pair rows)
	isPair     bool
	leftID     string // group leader device ID
	rightID    string // secondary device ID
	leftState  raop.SessionState
	rightState raop.SessionState
}

// NewDeviceList creates a new device list widget.
func NewDeviceList(manager *service.Manager) *DeviceList {
	listBox := gtk.NewListBox()
	listBox.AddCSSClass("device-list")
	listBox.SetSelectionMode(gtk.SelectionNone)

	return &DeviceList{
		ListBox:     listBox,
		manager:     manager,
		rows:        make(map[string]*deviceRow),
		deviceToRow: make(map[string]string),
	}
}

// UpdateDevices updates the list with newly discovered devices.
func (dl *DeviceList) UpdateDevices(devices []discovery.AirPlayDevice) {
	// Group devices by GroupID to find stereo pairs
	groups := make(map[string][]discovery.AirPlayDevice) // gid → devices
	for _, dev := range devices {
		if dev.GroupID != "" {
			groups[dev.GroupID] = append(groups[dev.GroupID], dev)
		}
	}

	// Determine which row IDs should exist
	wantIDs := make(map[string]bool)

	// All individual devices get a row
	for _, dev := range devices {
		wantIDs[dev.ID] = true
	}

	// Stereo pairs (exactly 2 devices with same gid) get an extra combined row
	for gid, members := range groups {
		if len(members) == 2 {
			wantIDs["pair-"+gid] = true
		}
	}

	// Remove rows that no longer exist
	for id, row := range dl.rows {
		if !wantIDs[id] {
			dl.ListBox.Remove(row.row)
			delete(dl.rows, id)
		}
	}

	// Clear deviceToRow mapping (rebuilt below)
	dl.deviceToRow = make(map[string]string)

	// Add/update stereo pair rows (insert at the top)
	for gid, members := range groups {
		if len(members) != 2 {
			continue
		}

		pairID := "pair-" + gid

		// Determine leader/secondary
		var leader, secondary discovery.AirPlayDevice
		if members[0].IsGroupLeader {
			leader, secondary = members[0], members[1]
		} else {
			leader, secondary = members[1], members[0]
		}

		// Map both device IDs to this pair row
		dl.deviceToRow[leader.ID] = pairID
		dl.deviceToRow[secondary.ID] = pairID

		groupName := unescapeDNS(leader.GroupName)
		if groupName == "" {
			groupName = unescapeDNS(leader.Name)
		}

		if existing, ok := dl.rows[pairID]; ok {
			existing.nameLabel.SetText(groupName)
			existing.infoLabel.SetText("Stereo Pair")
			existing.leftID = leader.ID
			existing.rightID = secondary.ID
			existing.device = leader
		} else {
			dl.addPairRow(pairID, groupName, leader, secondary)
		}
	}

	// Add/update individual device rows
	for _, dev := range devices {
		if existing, ok := dl.rows[dev.ID]; ok {
			existing.nameLabel.SetText(unescapeDNS(dev.Name))
			infoText := friendlyModel(dev.Model)
			if infoText == "" || infoText == dev.Model {
				infoText = dev.Host
			}
			existing.infoLabel.SetText(infoText)
			existing.device = dev
		} else {
			dl.addDeviceRow(dev)
		}
	}
}

// UpdateSessionState updates the connect/disconnect button for a device.
func (dl *DeviceList) UpdateSessionState(deviceID string, state raop.SessionState) {
	// Update individual device row
	if row, ok := dl.rows[deviceID]; ok {
		dl.updateRowState(row, state)
	}

	// Also update the pair row if this device is part of a pair
	if pairID, ok := dl.deviceToRow[deviceID]; ok {
		if pairRow, ok := dl.rows[pairID]; ok {
			// Update the appropriate side's state
			if deviceID == pairRow.leftID {
				pairRow.leftState = state
			} else {
				pairRow.rightState = state
			}
			dl.updatePairRowState(pairRow)
		}
	}
}

func (dl *DeviceList) updateRowState(row *deviceRow, state raop.SessionState) {
	switch state {
	case raop.StateConnecting:
		row.button.SetLabel("Connecting...")
		row.button.SetSensitive(false)
	case raop.StateConnected, raop.StateStreaming:
		row.connected = true
		row.button.SetLabel("Disconnect")
		row.button.SetSensitive(true)
		row.button.RemoveCSSClass("connect-btn")
		row.button.AddCSSClass("disconnect-btn")
	case raop.StateDisconnected, raop.StateError:
		row.connected = false
		row.button.SetLabel("Connect")
		row.button.SetSensitive(true)
		row.button.RemoveCSSClass("disconnect-btn")
		row.button.AddCSSClass("connect-btn")
	}
}

func (dl *DeviceList) updatePairRowState(row *deviceRow) {
	leftOK := row.leftState == raop.StateConnected || row.leftState == raop.StateStreaming
	rightOK := row.rightState == raop.StateConnected || row.rightState == raop.StateStreaming
	leftConnecting := row.leftState == raop.StateConnecting
	rightConnecting := row.rightState == raop.StateConnecting

	if leftOK && rightOK {
		row.connected = true
		row.button.SetLabel("Disconnect")
		row.button.SetSensitive(true)
		row.button.RemoveCSSClass("connect-btn")
		row.button.AddCSSClass("disconnect-btn")
	} else if leftConnecting || rightConnecting {
		row.button.SetLabel("Connecting...")
		row.button.SetSensitive(false)
	} else {
		row.connected = false
		row.button.SetLabel("Connect")
		row.button.SetSensitive(true)
		row.button.RemoveCSSClass("disconnect-btn")
		row.button.AddCSSClass("connect-btn")
	}
}

func (dl *DeviceList) addPairRow(pairID, groupName string, leader, secondary discovery.AirPlayDevice) {
	hbox := gtk.NewBox(gtk.OrientationHorizontal, 12)
	hbox.AddCSSClass("device-row")
	hbox.AddCSSClass("pair-row")

	// Stereo pair icon (based on leader's model)
	icon := gtk.NewImageFromIconName(deviceIcon(leader.Model))
	icon.SetPixelSize(28)
	icon.SetVAlign(gtk.AlignCenter)
	icon.AddCSSClass("device-icon")

	// Name + info
	vbox := gtk.NewBox(gtk.OrientationVertical, 2)
	vbox.SetHExpand(true)

	nameLabel := gtk.NewLabel(unescapeDNS(groupName))
	nameLabel.AddCSSClass("device-name")
	nameLabel.SetHAlign(gtk.AlignStart)

	infoLabel := gtk.NewLabel("Stereo Pair")
	infoLabel.AddCSSClass("device-info")
	infoLabel.SetHAlign(gtk.AlignStart)

	vbox.Append(nameLabel)
	vbox.Append(infoLabel)

	// Right-side action box (same width as individual rows' dropdown+button)
	actionBox := gtk.NewBox(gtk.OrientationHorizontal, 8)
	actionBox.SetHAlign(gtk.AlignEnd)

	button := gtk.NewButtonWithLabel("Connect")
	button.AddCSSClass("connect-btn")
	button.SetVAlign(gtk.AlignCenter)
	button.SetSizeRequest(100, -1)

	actionBox.Append(button)

	row := &deviceRow{
		nameLabel: nameLabel,
		infoLabel: infoLabel,
		button:    button,
		icon:      icon,
		device:    leader,
		isPair:    true,
		leftID:    leader.ID,
		rightID:   secondary.ID,
	}

	leftID := leader.ID
	rightID := secondary.ID

	button.ConnectClicked(func() {
		if row.connected {
			go func() {
				dl.manager.DisconnectDevice(leftID)
				dl.manager.DisconnectDevice(rightID)
			}()
		} else {
			// Auto-assign L/R and connect both
			dl.manager.SetChannelMode(leftID, audio.ChannelLeft)
			dl.manager.SetChannelMode(rightID, audio.ChannelRight)
			go func() {
				if err := dl.manager.ConnectDevice(leftID); err != nil {
					log.Printf("ui: connect L (%s) error: %v", leader.Name, err)
				}
			}()
			go func() {
				if err := dl.manager.ConnectDevice(rightID); err != nil {
					log.Printf("ui: connect R (%s) error: %v", secondary.Name, err)
				}
			}()
		}
	})

	hbox.Append(icon)
	hbox.Append(vbox)
	hbox.Append(actionBox)

	listBoxRow := gtk.NewListBoxRow()
	listBoxRow.SetChild(hbox)

	// Insert pair rows at the top
	dl.ListBox.Prepend(listBoxRow)

	row.row = listBoxRow
	dl.rows[pairID] = row
}

func (dl *DeviceList) addDeviceRow(dev discovery.AirPlayDevice) {
	hbox := gtk.NewBox(gtk.OrientationHorizontal, 12)
	hbox.AddCSSClass("device-row")

	// Device-specific icon
	icon := gtk.NewImageFromIconName(deviceIcon(dev.Model))
	icon.SetPixelSize(24)
	icon.SetVAlign(gtk.AlignCenter)
	icon.AddCSSClass("device-icon")

	// Name + info
	vbox := gtk.NewBox(gtk.OrientationVertical, 2)
	vbox.SetHExpand(true)

	nameLabel := gtk.NewLabel(unescapeDNS(dev.Name))
	nameLabel.AddCSSClass("device-name")
	nameLabel.SetHAlign(gtk.AlignStart)

	infoText := friendlyModel(dev.Model)
	if infoText == "" || infoText == dev.Model {
		infoText = dev.Host
	}
	infoLabel := gtk.NewLabel(infoText)
	infoLabel.AddCSSClass("device-info")
	infoLabel.SetHAlign(gtk.AlignStart)

	vbox.Append(nameLabel)
	vbox.Append(infoLabel)

	deviceID := dev.ID

	// Right-side action box (dropdown + button, aligned with pair rows)
	actionBox := gtk.NewBox(gtk.OrientationHorizontal, 8)
	actionBox.SetHAlign(gtk.AlignEnd)

	// Channel mode dropdown
	chanModel := gtk.NewStringList([]string{"Stereo", "Left", "Right"})
	chanDrop := gtk.NewDropDown(chanModel, nil)
	chanDrop.SetSelected(uint(dl.manager.GetChannelMode(deviceID)))
	chanDrop.AddCSSClass("channel-drop")
	chanDrop.SetVAlign(gtk.AlignCenter)
	chanDrop.SetSizeRequest(90, -1)

	chanDrop.NotifyProperty("selected", func() {
		mode := audio.ChannelMode(chanDrop.Selected())
		dl.manager.SetChannelMode(deviceID, mode)
	})

	// Connect button
	button := gtk.NewButtonWithLabel("Connect")
	button.AddCSSClass("connect-btn")
	button.SetVAlign(gtk.AlignCenter)
	button.SetSizeRequest(100, -1)

	row := &deviceRow{
		nameLabel: nameLabel,
		infoLabel: infoLabel,
		button:    button,
		chanDrop:  chanDrop,
		icon:      icon,
		device:    dev,
		connected: false,
	}

	button.ConnectClicked(func() {
		if row.connected {
			go dl.manager.DisconnectDevice(deviceID)
		} else {
			go func() {
				if err := dl.manager.ConnectDevice(deviceID); err != nil {
					log.Printf("ui: connect error: %v", err)
				}
			}()
		}
	})

	actionBox.Append(chanDrop)
	actionBox.Append(button)

	hbox.Append(icon)
	hbox.Append(vbox)
	hbox.Append(actionBox)

	listBoxRow := gtk.NewListBoxRow()
	listBoxRow.SetChild(hbox)
	dl.ListBox.Append(listBoxRow)

	row.row = listBoxRow
	dl.rows[dev.ID] = row
}
