package gui

import (
"fmt"
"strconv"

"wled-simulator/internal/config"

"fyne.io/fyne/v2"
"fyne.io/fyne/v2/container"
"fyne.io/fyne/v2/layout"
"fyne.io/fyne/v2/theme"
"fyne.io/fyne/v2/widget"
)

// Sidebar holds the settings panel (config form only).
type Sidebar struct {
Container fyne.CanvasObject

// Config form fields
rowsEntry       *widget.Entry
colsEntry       *widget.Entry
wiringSelect    *widget.Select
httpAddrEntry   *widget.Entry
ddpPortEntry    *widget.Entry
initColorEntry  *widget.Entry
nameEntry       *widget.Entry
verboseCheck    *widget.Check
rgbwCheck       *widget.Check
recFormatSelect *widget.Select
recDurEntry     *widget.Entry
recFPSEntry     *widget.Entry

onApply  func(config.Config)
onCancel func()
}

func NewSidebar(
cfg config.Config,
onApply func(config.Config),
onCancel func(),
) *Sidebar {
sb := &Sidebar{
onApply:  onApply,
onCancel: onCancel,
}

sb.buildConfigForm(cfg)

configSection := sb.section("Configuration", sb.buildConfigLayout())
content := container.NewVBox(configSection)
scroll := container.NewVScroll(content)
scroll.SetMinSize(fyne.NewSize(300, 0))
sb.Container = scroll
return sb
}

func numEntry(val int) *widget.Entry {
e := widget.NewEntry()
e.SetText(strconv.Itoa(val))
e.Validator = func(s string) error {
if s == "" {
return fmt.Errorf("required")
}
_, err := strconv.Atoi(s)
return err
}
return e
}

func (sb *Sidebar) buildConfigForm(cfg config.Config) {
sb.rowsEntry = numEntry(cfg.Rows)
sb.colsEntry = numEntry(cfg.Cols)

sb.wiringSelect = widget.NewSelect([]string{"row", "col"}, nil)
sb.wiringSelect.SetSelected(cfg.Wiring)

sb.httpAddrEntry = widget.NewEntry()
sb.httpAddrEntry.SetText(cfg.HTTPAddress)

sb.ddpPortEntry = numEntry(cfg.DDPPort)

sb.initColorEntry = widget.NewEntry()
sb.initColorEntry.SetText(cfg.InitColor)

sb.nameEntry = widget.NewEntry()
sb.nameEntry.SetText(cfg.Name)

sb.verboseCheck = widget.NewCheck("Verbose", nil)
sb.verboseCheck.SetChecked(cfg.Verbose)

sb.rgbwCheck = widget.NewCheck("RGBW", nil)
sb.rgbwCheck.SetChecked(cfg.RGBW)

sb.recFormatSelect = widget.NewSelect([]string{"gif", "mp4", "both"}, nil)
sb.recFormatSelect.SetSelected(cfg.RecordFormat)

sb.recDurEntry = numEntry(cfg.RecordDuration)
sb.recFPSEntry = numEntry(cfg.RecordFPS)
}

func (sb *Sidebar) buildConfigLayout() *fyne.Container {
applyBtn := widget.NewButtonWithIcon("Apply & Save", theme.DocumentSaveIcon(), func() {
if sb.onApply != nil {
sb.onApply(sb.readConfig())
}
})
applyBtn.Importance = widget.HighImportance

cancelBtn := widget.NewButtonWithIcon("Cancel", theme.CancelIcon(), func() {
if sb.onCancel != nil {
sb.onCancel()
}
})

return container.NewVBox(
sb.formRow("Rows", sb.rowsEntry),
sb.formRow("Cols", sb.colsEntry),
sb.formRow("Wiring", sb.wiringSelect),
sb.formRow("HTTP Addr", sb.httpAddrEntry),
sb.formRow("DDP Port", sb.ddpPortEntry),
sb.formRow("Init Color", sb.initColorEntry),
sb.formRow("Name", sb.nameEntry),
container.NewHBox(sb.verboseCheck, sb.rgbwCheck),
widget.NewSeparator(),
sb.formRow("Rec Format", sb.recFormatSelect),
sb.formRow("Rec Dur (s)", sb.recDurEntry),
sb.formRow("Rec FPS", sb.recFPSEntry),
layout.NewSpacer(),
container.NewGridWithColumns(2, cancelBtn, applyBtn),
)
}

func (sb *Sidebar) readConfig() config.Config {
rows, _ := strconv.Atoi(sb.rowsEntry.Text)
cols, _ := strconv.Atoi(sb.colsEntry.Text)
ddpPort, _ := strconv.Atoi(sb.ddpPortEntry.Text)
recDur, _ := strconv.Atoi(sb.recDurEntry.Text)
recFPS, _ := strconv.Atoi(sb.recFPSEntry.Text)

return config.Config{
Rows:           rows,
Cols:           cols,
Wiring:         sb.wiringSelect.Selected,
HTTPAddress:    sb.httpAddrEntry.Text,
DDPPort:        ddpPort,
InitColor:      sb.initColorEntry.Text,
Name:           sb.nameEntry.Text,
Verbose:        sb.verboseCheck.Checked,
RGBW:           sb.rgbwCheck.Checked,
RecordFormat:   sb.recFormatSelect.Selected,
RecordDuration: recDur,
RecordFPS:      recFPS,
}
}

func (sb *Sidebar) UpdateConfig(cfg config.Config) {
sb.rowsEntry.SetText(strconv.Itoa(cfg.Rows))
sb.colsEntry.SetText(strconv.Itoa(cfg.Cols))
sb.wiringSelect.SetSelected(cfg.Wiring)
sb.httpAddrEntry.SetText(cfg.HTTPAddress)
sb.ddpPortEntry.SetText(strconv.Itoa(cfg.DDPPort))
sb.initColorEntry.SetText(cfg.InitColor)
sb.nameEntry.SetText(cfg.Name)
sb.verboseCheck.SetChecked(cfg.Verbose)
sb.rgbwCheck.SetChecked(cfg.RGBW)
sb.recFormatSelect.SetSelected(cfg.RecordFormat)
sb.recDurEntry.SetText(strconv.Itoa(cfg.RecordDuration))
sb.recFPSEntry.SetText(strconv.Itoa(cfg.RecordFPS))
}

func (sb *Sidebar) section(title string, content fyne.CanvasObject) *fyne.Container {
header := widget.NewLabel(title)
header.TextStyle = fyne.TextStyle{Bold: true}
return container.NewVBox(header, widget.NewSeparator(), content)
}

func (sb *Sidebar) formRow(label string, input fyne.CanvasObject) *fyne.Container {
l := widget.NewLabel(label)
return container.NewBorder(nil, nil, l, nil, input)
}
