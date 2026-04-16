package gui

import (
"fmt"
"image/color"
"strconv"
"time"

"wled-simulator/internal/config"
"wled-simulator/internal/recorder"
"wled-simulator/internal/state"

"fyne.io/fyne/v2"
"fyne.io/fyne/v2/canvas"
"fyne.io/fyne/v2/container"
"fyne.io/fyne/v2/layout"
"fyne.io/fyne/v2/theme"
"fyne.io/fyne/v2/widget"
)

// Sidebar holds the settings window content.
type Sidebar struct {
Container fyne.CanvasObject

state    *state.LEDState
recorder *recorder.Recorder

startStopBtn *widget.Button
statusLabel  *widget.Label
running      bool

recordBtn   *widget.Button
recordLabel *widget.Label

ddpRateText  *canvas.Text
httpRateText *canvas.Text
uptimeText   *canvas.Text
ledCountText *canvas.Text
liveCircle   *canvas.Circle

prevDDP       uint64
prevHTTP      uint64
lastDDPStr    string
lastHTTPStr   string
lastUptimeStr string
lastLEDStr    string
lastLive      bool

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

onStartStop func(start bool) error
onApply     func(config.Config) error
}

func NewSidebar(
s *state.LEDState,
cfg config.Config,
rec *recorder.Recorder,
onStartStop func(start bool) error,
onApply func(config.Config) error,
) *Sidebar {
sb := &Sidebar{
state:       s,
recorder:    rec,
running:     true,
onStartStop: onStartStop,
onApply:     onApply,
}

sb.buildControls()
sb.buildStats()
sb.buildConfigForm(cfg)

controlSection := sb.section("Controls", container.NewVBox(
sb.startStopBtn, sb.statusLabel, sb.recordBtn, sb.recordLabel,
))
statsSection := sb.section("Statistics", container.NewVBox(
sb.statRow("DDP pkts/s", sb.ddpRateText),
sb.statRow("HTTP req/s", sb.httpRateText),
sb.statRow("Uptime", sb.uptimeText),
sb.statRow("Total LEDs", sb.ledCountText),
sb.statRow("Live", sb.liveCircle),
))
configSection := sb.section("Configuration", sb.buildConfigLayout())

content := container.NewVBox(controlSection, statsSection, configSection)
scroll := container.NewVScroll(content)
scroll.SetMinSize(fyne.NewSize(300, 0))
sb.Container = scroll
return sb
}

func (sb *Sidebar) buildControls() {
sb.statusLabel = widget.NewLabel("● Running")
sb.statusLabel.Importance = widget.SuccessImportance

sb.startStopBtn = widget.NewButtonWithIcon("Stop", theme.MediaStopIcon(), func() {
sb.startStopBtn.Disable()
wantStart := !sb.running
sb.statusLabel.SetText("● Working...")
sb.statusLabel.Importance = widget.WarningImportance
sb.statusLabel.Refresh()

go func() {
err := sb.onStartStop(wantStart)
fyne.Do(func() {
if err != nil {
sb.statusLabel.SetText("Error: " + err.Error())
sb.statusLabel.Importance = widget.DangerImportance
} else if wantStart {
sb.running = true
sb.startStopBtn.SetText("Stop")
sb.startStopBtn.SetIcon(theme.MediaStopIcon())
sb.statusLabel.SetText("● Running")
sb.statusLabel.Importance = widget.SuccessImportance
} else {
sb.running = false
sb.startStopBtn.SetText("Start")
sb.startStopBtn.SetIcon(theme.MediaPlayIcon())
sb.statusLabel.SetText("● Stopped")
sb.statusLabel.Importance = widget.DangerImportance
}
sb.statusLabel.Refresh()
sb.startStopBtn.Enable()
})
}()
})

sb.recordLabel = widget.NewLabel("Ready")
sb.recordBtn = widget.NewButtonWithIcon("Record", theme.MediaRecordIcon(), func() {
if sb.recorder == nil {
return
}
if sb.recorder.IsRecording() {
filename, err := sb.recorder.Stop()
if err != nil {
sb.recordLabel.SetText("Error: " + err.Error())
return
}
sb.recordBtn.SetText("Record")
sb.recordBtn.SetIcon(theme.MediaRecordIcon())
sb.recordLabel.SetText("Saved: " + filename)
} else {
if err := sb.recorder.Start(); err != nil {
sb.recordLabel.SetText("Error: " + err.Error())
return
}
sb.recordBtn.SetText("Stop Rec")
sb.recordBtn.SetIcon(theme.MediaStopIcon())
sb.recordLabel.SetText("Recording...")
}
})
}

func (sb *Sidebar) buildStats() {
sb.ddpRateText = canvas.NewText("0", color.White)
sb.ddpRateText.TextSize = 14
sb.ddpRateText.Alignment = fyne.TextAlignTrailing

sb.httpRateText = canvas.NewText("0", color.White)
sb.httpRateText.TextSize = 14
sb.httpRateText.Alignment = fyne.TextAlignTrailing

sb.uptimeText = canvas.NewText("0s", color.White)
sb.uptimeText.TextSize = 14
sb.uptimeText.Alignment = fyne.TextAlignTrailing

sb.ledCountText = canvas.NewText(fmt.Sprintf("%d", sb.state.LEDCount()), color.White)
sb.ledCountText.TextSize = 14
sb.ledCountText.Alignment = fyne.TextAlignTrailing

sb.liveCircle = canvas.NewCircle(color.RGBA{128, 128, 128, 255})
sb.liveCircle.Resize(fyne.NewSize(12, 12))
}

func (sb *Sidebar) RefreshStats() {
curDDP := sb.state.DDPCount()
curHTTP := sb.state.HTTPCount()
ddpRate := curDDP - sb.prevDDP
httpRate := curHTTP - sb.prevHTTP
sb.prevDDP = curDDP
sb.prevHTTP = curHTTP

ddpStr := fmt.Sprintf("%d", ddpRate)
httpStr := fmt.Sprintf("%d", httpRate)
uptimeStr := formatDuration(time.Since(sb.state.StartTime()).Truncate(time.Second))
ledStr := fmt.Sprintf("%d", sb.state.LEDCount())
live := sb.state.IsLive()

if ddpStr == sb.lastDDPStr && httpStr == sb.lastHTTPStr &&
uptimeStr == sb.lastUptimeStr && ledStr == sb.lastLEDStr &&
live == sb.lastLive {
return
}
sb.lastDDPStr = ddpStr
sb.lastHTTPStr = httpStr
sb.lastUptimeStr = uptimeStr
sb.lastLEDStr = ledStr
sb.lastLive = live

fyne.Do(func() {
sb.ddpRateText.Text = ddpStr
sb.ddpRateText.Refresh()
sb.httpRateText.Text = httpStr
sb.httpRateText.Refresh()
sb.uptimeText.Text = uptimeStr
sb.uptimeText.Refresh()
sb.ledCountText.Text = ledStr
sb.ledCountText.Refresh()

if live {
sb.liveCircle.FillColor = color.RGBA{0, 200, 0, 255}
} else {
sb.liveCircle.FillColor = color.RGBA{128, 128, 128, 255}
}
sb.liveCircle.Refresh()

if sb.recorder != nil && !sb.recorder.IsRecording() && sb.recordBtn.Text == "Stop Rec" {
sb.recordBtn.SetText("Record")
sb.recordBtn.SetIcon(theme.MediaRecordIcon())
sb.recordLabel.SetText("Done (auto-stop)")
}
})
}

func (sb *Sidebar) SetRunning(running bool) {
sb.running = running
fyne.Do(func() {
if running {
sb.startStopBtn.SetText("Stop")
sb.startStopBtn.SetIcon(theme.MediaStopIcon())
sb.statusLabel.SetText("● Running")
sb.statusLabel.Importance = widget.SuccessImportance
} else {
sb.startStopBtn.SetText("Start")
sb.startStopBtn.SetIcon(theme.MediaPlayIcon())
sb.statusLabel.SetText("● Stopped")
sb.statusLabel.Importance = widget.DangerImportance
}
sb.statusLabel.Refresh()
})
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
applyBtn := widget.NewButtonWithIcon("Apply & Save", theme.DocumentSaveIcon(), nil)
applyBtn.Importance = widget.HighImportance
applyBtn.OnTapped = func() {
applyBtn.Disable()
sb.statusLabel.SetText("● Applying...")
sb.statusLabel.Importance = widget.WarningImportance
sb.statusLabel.Refresh()
cfg := sb.readConfig()

go func() {
var err error
if sb.onApply != nil {
err = sb.onApply(cfg)
}
fyne.Do(func() {
if err != nil {
sb.statusLabel.SetText("Apply error: " + err.Error())
sb.statusLabel.Importance = widget.DangerImportance
} else {
sb.statusLabel.SetText("● Applied")
sb.statusLabel.Importance = widget.SuccessImportance
}
sb.statusLabel.Refresh()
applyBtn.Enable()
})
}()
}

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
applyBtn,
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

func (sb *Sidebar) statRow(label string, value fyne.CanvasObject) *fyne.Container {
l := canvas.NewText(label, color.RGBA{180, 180, 180, 255})
l.TextSize = 13
return container.NewHBox(l, layout.NewSpacer(), value)
}

func (sb *Sidebar) formRow(label string, input fyne.CanvasObject) *fyne.Container {
l := widget.NewLabel(label)
return container.NewBorder(nil, nil, l, nil, input)
}

func formatDuration(d time.Duration) string {
h := int(d.Hours())
m := int(d.Minutes()) % 60
s := int(d.Seconds()) % 60
if h > 0 {
return fmt.Sprintf("%dh%02dm%02ds", h, m, s)
}
if m > 0 {
return fmt.Sprintf("%dm%02ds", m, s)
}
return fmt.Sprintf("%ds", s)
}
