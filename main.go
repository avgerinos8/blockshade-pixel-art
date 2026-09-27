package main

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const minHeight = 29
const minWidth = 50
const sidebarWidth = 37

type BrushSet struct {
	Name    string
	Brushes []string
}

var brushSets = []BrushSet{
	{"IBM Basic", []string{"\u2591", "\u2592", "\u2593", "\u2588"}}, // ░, ▒, ▓, █
	{"IBM Ext.", []string{"\u2580", "\u2584", "\u258c", "\u2590"}},  // ▀, ▄, ▌, ▐
	{"Blocks", []string{"\u2596", "\u2597", "\u2598", "\u259d"}},    // ▖, ▗, ▘, ▝
}

var allChars = func() []string {
	chars := []string{" "}
	for _, set := range brushSets {
		chars = append(chars, set.Brushes...)
	}
	return chars
}()

type SidebarButton struct {
	X      int
	Y      int
	W      int
	Label  string
	Action string
	Color  string
}

// ── Structs & Core State ─────────────────────────────── ⊃

type model struct {
	termWidth  int
	termHeight int

	canvasWidth  int
	canvasHeight int
	canvas       [][]string
	zoomLevel    int

	selectedChar   string
	activeBrushSet int

	widthInput  textinput.Model
	heightInput textinput.Model

	history      [][][]string
	redoStack    [][][]string
	lastSaveTime time.Time

	replaceFromIdx int
	replaceToIdx   int
	replaceChance  int
	replacePattern string

	isMouseDown      bool
	isRightClickDrag bool
	message          string
	lastMouseX       int
	lastMouseY       int
}



// ── Application Entry Point ─────────────────────────────── ⊃

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen(), tea.WithMouseAllMotion())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

// ── Core Bubble Tea Methods ─────────────────────────────── ⊃

func initialModel() model {
	validator := func(s string) error {
		for _, r := range s {
			if !unicode.IsDigit(r) {
				return fmt.Errorf("digits only")
			}
		}
		return nil
	}

	wInput := textinput.New()
	wInput.Prompt = ""
	wInput.Placeholder = ""
	wInput.CharLimit = 3
	wInput.Width = 3
	wInput.SetValue("40")
	wInput.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF"))
	wInput.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#D7005F"))
	wInput.Validate = validator

	hInput := textinput.New()
	hInput.Prompt = ""
	hInput.Placeholder = ""
	hInput.CharLimit = 3
	hInput.Width = 3
	hInput.SetValue("12")
	hInput.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF"))
	hInput.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#D7005F"))
	hInput.Validate = validator

	m := model{
		canvasWidth:    40,
		canvasHeight:   12,
		zoomLevel:      1,
		activeBrushSet: 0,
		selectedChar:   "\u2588",
		widthInput:     wInput,
		heightInput:    hInput,
		history:        make([][][]string, 0),
		redoStack:      make([][][]string, 0),
		lastSaveTime:   time.Time{},

		replaceFromIdx: 4,
		replaceToIdx:   1,
		replaceChance:  100,
		replacePattern: "random",
	}

	m = m.initCanvas(40, 12)
	return m
}

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.termWidth = msg.Width
		m.termHeight = msg.Height
		return m, nil

	case tea.KeyMsg:
		if m.widthInput.Focused() {
			if msg.Type == tea.KeyEnter || msg.Type == tea.KeyTab {
				m.widthInput.Blur()
				m.heightInput.Focus()
				return m, textinput.Blink
			}
			var cmd tea.Cmd
			m.widthInput, cmd = m.widthInput.Update(msg)

			if val, err := strconv.Atoi(m.widthInput.Value()); err == nil && val > 500 {
				m.widthInput.SetValue("500")
			}
			return m, cmd
		}

		if m.heightInput.Focused() {
			if msg.Type == tea.KeyEnter || msg.Type == tea.KeyTab {
				m.heightInput.Blur()
				m.widthInput.Focus()
				return m, textinput.Blink
			}
			var cmd tea.Cmd
			m.heightInput, cmd = m.heightInput.Update(msg)

			if val, err := strconv.Atoi(m.heightInput.Value()); err == nil && val > 500 {
				m.heightInput.SetValue("500")
			}
			return m, cmd
		}

		switch msg.Type {
		case tea.KeyCtrlZ:
			m = m.undo()
			return m, nil
		case tea.KeyCtrlY:
			m = m.redo()
			return m, nil
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		}

	case tea.MouseMsg:
		oldX := m.lastMouseX
		oldY := m.lastMouseY
		m.lastMouseX = msg.X
		m.lastMouseY = msg.Y

		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			m.message = ""
			m.isMouseDown = true
			m.isRightClickDrag = false
			m.widthInput.Blur()
			m.heightInput.Blur()
			m = m.handleMouse(msg.X, msg.Y, false, false)
		} else if msg.Action == tea.MouseActionRelease && (msg.Button == tea.MouseButtonLeft || msg.Button == tea.MouseButtonRight || msg.Button == tea.MouseButtonNone) {
			m.isMouseDown = false
			m.isRightClickDrag = false
		} else if msg.Action == tea.MouseActionMotion && m.isMouseDown {
			m = m.drawLine(oldX, oldY, msg.X, msg.Y, m.isRightClickDrag)
		} else if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonRight {
			m.message = ""
			m.isMouseDown = true
			m.isRightClickDrag = true
			m.widthInput.Blur()
			m.heightInput.Blur()
			m = m.handleMouse(msg.X, msg.Y, false, true)
		} else if msg.Action == tea.MouseActionPress && (msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown) {
			delta := 1
			if msg.Button == tea.MouseButtonWheelDown {
				delta = -1
			}

			inX := msg.X >= 17 && msg.X <= 21 && msg.Y >= 2 && msg.Y <= 4
			inY := msg.X >= 25 && msg.X <= 29 && msg.Y >= 2 && msg.Y <= 4

			if inX {
				if val, err := strconv.Atoi(m.widthInput.Value()); err == nil {
					newVal := val + delta
					if newVal < 1 { newVal = 1 }
					if newVal > 500 { newVal = 500 }
					m.widthInput.SetValue(fmt.Sprintf("%d", newVal))
				}
			} else if inY {
				if val, err := strconv.Atoi(m.heightInput.Value()); err == nil {
					newVal := val + delta
					if newVal < 1 { newVal = 1 }
					if newVal > 500 { newVal = 500 }
					m.heightInput.SetValue(fmt.Sprintf("%d", newVal))
				}
			} else {
				var cycleChars []string
				for _, set := range brushSets {
					cycleChars = append(cycleChars, set.Brushes...)
				}

				currentIdx := 0
				for i, char := range cycleChars {
					if char == m.selectedChar {
						currentIdx = i
						break
					}
				}
				newIdx := currentIdx - delta
				if newIdx < 0 {
					newIdx = len(cycleChars) - 1
				} else if newIdx >= len(cycleChars) {
					newIdx = 0
				}
				newChar := cycleChars[newIdx]
				m.selectedChar = newChar

				for i, set := range brushSets {
					for _, b := range set.Brushes {
						if b == newChar {
							m.activeBrushSet = i
							break
						}
					}
				}
			}
		}
	}

	return m, nil
}

func (m model) View() string {
	if m.termHeight > 0 && m.termHeight < minHeight {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("9")).
			Padding(1).
			Render(fmt.Sprintf("Please maximize your viewport. (Needs at least %d lines height)", minHeight))
	}
	if m.termWidth > 0 && m.termWidth < minWidth {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("9")).
			Padding(1).
			Render(fmt.Sprintf("Please maximize your viewport. (Needs at least %d cols width)", minWidth))
	}

	// 1. Render Toolbar
	toolbar := make([]string, m.termHeight)
	defaultBg := lipgloss.Color("#222222")
	activeBg := lipgloss.Color("#D7005F")
	sidebarBorder := lipgloss.NewStyle().Foreground(lipgloss.Color("#555555")).Render("│")

	buttonsByY := make(map[int][]SidebarButton)
	for _, btn := range m.getSidebarButtons() {
		buttonsByY[btn.Y] = append(buttonsByY[btn.Y], btn)
	}

	for y := 0; y < m.termHeight; y++ {
		if y == 3 {
			wView := m.widthInput.View()
			hView := m.heightInput.View()

			newBtnStr := lipgloss.NewStyle().Background(lipgloss.Color("#0087AF")).Foreground(lipgloss.Color("#FFFFFF")).Render(" [New Canvas] ")
			bg := lipgloss.NewStyle().Background(defaultBg)
			textColor := lipgloss.NewStyle().Background(defaultBg).Foreground(lipgloss.Color("#AAAAAA"))

			p1 := textColor.Render(" X:[")
			p2 := bg.Render(wView)
			p3 := textColor.Render("] Y:[")
			p4 := bg.Render(hView)
			p5 := textColor.Render("]")

			fullStr := newBtnStr + p1 + p2 + p3 + p4 + p5
			visW := ansi.StringWidth(fullStr)
			if visW < sidebarWidth {
				fullStr += bg.Render(strings.Repeat(" ", sidebarWidth-visW))
			}
			toolbar[y] = fullStr + sidebarBorder
			continue
		}

		if btns, ok := buttonsByY[y]; ok {
			var rowStr string
			currentX := 0
			for _, btn := range btns {
				if currentX < btn.X {
					rowStr += lipgloss.NewStyle().Background(defaultBg).Render(strings.Repeat(" ", btn.X-currentX))
					currentX = btn.X
				}

				isSelected := false
				if strings.HasPrefix(btn.Action, "brush") {
					idx := int(btn.Action[5] - '0')
					if m.selectedChar == brushSets[m.activeBrushSet].Brushes[idx] {
						isSelected = true
					}
				} else if btn.Action == "erase" {
					if m.selectedChar == " " {
						isSelected = true
					}
				} else if btn.Action == "patCls" && m.replacePattern == "cluster" {
					isSelected = true
				} else if btn.Action == "patOrg" && m.replacePattern == "organic" {
					isSelected = true
				} else if btn.Action == "patRnd" && m.replacePattern == "random" {
					isSelected = true
				}

				label := btn.Label
				if isSelected {
					if strings.HasPrefix(label, " ") {
						label = "▶" + label[1:]
					} else {
						label = "▶" + label
					}
				}

				if ansi.StringWidth(label) > btn.W {
					label = ansi.Truncate(label, btn.W, "")
				}

				labelWidth := ansi.StringWidth(label)
				if labelWidth < btn.W {
					label += strings.Repeat(" ", btn.W-labelWidth)
				}

				style := lipgloss.NewStyle().Background(defaultBg)

				if btn.Action == "" {
					style = style.Foreground(lipgloss.Color(btn.Color))
				} else if isSelected {
					style = style.Background(activeBg).Foreground(lipgloss.Color("#FFFFFF")).Bold(true)
				} else {
					style = style.Foreground(lipgloss.Color(btn.Color))
				}

				rowStr += style.Render(label)
				currentX += btn.W
			}

			if currentX < sidebarWidth {
				rowStr += lipgloss.NewStyle().Background(defaultBg).Render(strings.Repeat(" ", sidebarWidth-currentX))
			}
			toolbar[y] = rowStr + sidebarBorder
		} else {
			toolbar[y] = lipgloss.NewStyle().Width(sidebarWidth).Background(defaultBg).Render("") + sidebarBorder
		}
	}

	msgY := m.termHeight - 5
	dbgY := m.termHeight - 4
	logoY1 := m.termHeight - 3
	logoY2 := m.termHeight - 2
	logoY3 := m.termHeight - 1

	if m.message != "" && msgY >= 0 && msgY < len(toolbar) {
		msgLabel := m.message
		if ansi.StringWidth(msgLabel) > sidebarWidth-2 {
			msgLabel = ansi.Truncate(msgLabel, sidebarWidth-2, "…")
		}
		style := lipgloss.NewStyle().Width(sidebarWidth).Background(defaultBg).Foreground(lipgloss.Color("#D70000")).PaddingLeft(1).Bold(true)
		toolbar[msgY] = style.Render(msgLabel) + sidebarBorder
	}

	if dbgY >= 0 && dbgY < len(toolbar) {
		dbgLabel := fmt.Sprintf("X:%d Y:%d", m.lastMouseX, m.lastMouseY)
		style := lipgloss.NewStyle().Width(sidebarWidth).Background(defaultBg).Foreground(lipgloss.Color("#888888")).PaddingLeft(1)
		toolbar[dbgY] = style.Render(dbgLabel) + sidebarBorder
	}

	if logoY1 >= 0 && logoY1 < len(toolbar) {
		style := lipgloss.NewStyle().Width(sidebarWidth).Background(defaultBg).Foreground(lipgloss.Color("#6e176eff"))
		toolbar[logoY1] = style.Render("  ╭╮ ╷ ╷   ╭─╮╷ ╷╭─╴╭─╴╭─╮╷╭╮╷╭─╮╭─╮") + sidebarBorder
	}
	if logoY2 >= 0 && logoY2 < len(toolbar) {
		style := lipgloss.NewStyle().Width(sidebarWidth).Background(defaultBg).Foreground(lipgloss.Color("#6e176eff"))
		toolbar[logoY2] = style.Render("  ├┴╮╰┬╯   ├─┤│╭╯│╶╮├╴ ├┬╯││╰┤│ │╰─╮") + sidebarBorder
	}
	if logoY3 >= 0 && logoY3 < len(toolbar) {
		style := lipgloss.NewStyle().Width(sidebarWidth).Background(defaultBg).Foreground(lipgloss.Color("#6e176eff"))
		toolbar[logoY3] = style.Render("  ╰─╯ ╵    ╵ ╵╰╯ ╰─╯╰─╴╵╰╴╵╵ ╵╰─╯╰─╯") + sidebarBorder
	}

	// 2. Render Canvas area
	canvasLines := make([]string, m.termHeight)
	viewW := m.canvasWidth * m.zoomLevel
	viewH := m.canvasHeight * m.zoomLevel

	borderColor := lipgloss.Color("#005FD7")
	borderStyle := lipgloss.NewStyle().Foreground(borderColor)

	for i := range canvasLines {
		if i == 0 {
			canvasLines[i] = borderStyle.Render("┌" + strings.Repeat("─", viewW) + "┐")
		} else if i == viewH+1 {
			canvasLines[i] = borderStyle.Render("└" + strings.Repeat("─", viewW) + "┘")
		} else if i > 0 && i <= viewH {
			canvasY := (i - 1) / m.zoomLevel
			subY := (i - 1) % m.zoomLevel
			var rowSb strings.Builder
			rowSb.WriteString(borderStyle.Render("│"))
			for x := 0; x < viewW; x++ {
				canvasX := x / m.zoomLevel
				subX := x % m.zoomLevel
				if canvasX < m.canvasWidth && canvasY < m.canvasHeight {
					char := m.canvas[canvasY][canvasX]
					if m.zoomLevel > 1 {
						char = getZoomedChar(char, subX, subY, m.zoomLevel)
					}
					rowSb.WriteString(char)
				} else {
					rowSb.WriteString(" ")
				}
			}
			rowSb.WriteString(borderStyle.Render("│"))
			canvasLines[i] = rowSb.String()
		} else {
			canvasLines[i] = ""
		}
	}

	// 3. Join Toolbar and Canvas
	var sb strings.Builder
	for y := 0; y < m.termHeight; y++ {
		var lineSb strings.Builder
		lineSb.WriteString(toolbar[y])
		if y < len(canvasLines) {
			lineSb.WriteString(canvasLines[y])
		}

		lineStr := lineSb.String()
		if ansi.StringWidth(lineStr) > m.termWidth {
			lineStr = ansi.Truncate(lineStr, m.termWidth, "") + "\x1b[0m"
		}

		sb.WriteString(lineStr)
		if y < m.termHeight-1 {
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// ── Mouse & Drawing Engine ─────────────────────────────── ⊃

func (m model) handleMouse(x, y int, isMotion bool, isRightClick bool) model {
	if x < sidebarWidth {
		if isMotion {
			return m
		}
		for _, btn := range m.getSidebarButtons() {
			if btn.Y == y && x >= btn.X && x < btn.X+btn.W {
				if btn.Action != "" {
					m = m.handleSidebarAction(btn.Action, isRightClick)
					if btn.Action != "focusX" && btn.Action != "focusY" {
						m.widthInput.Blur()
						m.heightInput.Blur()
					}
				}
			}
		}
	} else {
		contentX := x - (sidebarWidth + 2)
		contentY := y - 1
		if contentX >= 0 && contentY >= 0 {
			canvasX := contentX / m.zoomLevel
			canvasY := contentY / m.zoomLevel
			if canvasX < m.canvasWidth && canvasY < m.canvasHeight {
				targetChar := m.selectedChar
				if isRightClick {
					targetChar = " "
				}
				
				if m.canvas[canvasY][canvasX] != targetChar {
					m = m.saveState(false)
					m.canvas[canvasY][canvasX] = targetChar
				}
			}
		}
	}
	return m
}

func (m model) drawLine(x0, y0, x1, y1 int, isRightClick bool) model {
	dx := int(math.Abs(float64(x1 - x0)))
	dy := int(math.Abs(float64(y1 - y0)))
	sx := 1
	if x0 < x1 {
		sx = 1
	} else {
		sx = -1
	}
	sy := 1
	if y0 < y1 {
		sy = 1
	} else {
		sy = -1
	}
	err := dx - dy

	for {
		m = m.handleMouse(x0, y0, true, isRightClick)
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x0 += sx
		}
		if e2 < dx {
			err += dx
			y0 += sy
		}
	}
	return m
}

// ── Sidebar & UI Interactivity ─────────────────────────────── ⊃

func (m model) handleSidebarAction(action string, isRightClick bool) model {
	if isRightClick && action != "set" && action != "cycleFrom" && action != "cycleTo" {
		return m
	}

	switch action {
	case "set":
		if isRightClick {
			m.activeBrushSet = (m.activeBrushSet - 1 + len(brushSets)) % len(brushSets)
		} else {
			m.activeBrushSet = (m.activeBrushSet + 1) % len(brushSets)
		}
	case "cycleFrom":
		if isRightClick {
			m.replaceFromIdx = (m.replaceFromIdx - 1 + len(allChars)) % len(allChars)
		} else {
			m.replaceFromIdx = (m.replaceFromIdx + 1) % len(allChars)
		}
	case "cycleTo":
		if isRightClick {
			m.replaceToIdx = (m.replaceToIdx - 1 + len(allChars)) % len(allChars)
		} else {
			m.replaceToIdx = (m.replaceToIdx + 1) % len(allChars)
		}
	case "execReplace":
		m = m.executeReplace()
	case "chanceDec":
		if m.replaceChance > 0 {
			m.replaceChance -= 10
		}
	case "chanceInc":
		if m.replaceChance < 100 {
			m.replaceChance += 10
		}
	case "patRnd":
		m.replacePattern = "random"
	case "patCls":
		m.replacePattern = "cluster"
	case "patOrg":
		m.replacePattern = "organic"
	case "focusX":
		m.widthInput.Focus()
		m.heightInput.Blur()
	case "focusY":
		m.heightInput.Focus()
		m.widthInput.Blur()
	case "new":
		wStr := strings.TrimSpace(m.widthInput.Value())
		hStr := strings.TrimSpace(m.heightInput.Value())
		w, errW := strconv.Atoi(wStr)
		h, errH := strconv.Atoi(hStr)
		if errW == nil && errH == nil && w > 0 && h > 0 {
			if w > 500 {
				w = 500
				m.widthInput.SetValue("500")
			}
			if h > 500 {
				h = 500
				m.heightInput.SetValue("500")
			}
			m = m.saveState(true)
			m = m.initCanvas(w, h)
			m.message = "New canvas created"
		} else {
			m.message = "Invalid dims!"
		}
	case "copy":
		m = m.copyToClipboard()
	case "paste":
		m = m.pasteFromClipboard()
	case "undo":
		m = m.undo()
	case "redo":
		m = m.redo()
	case "zoomIn":
		if m.zoomLevel < 10 {
			m.zoomLevel++
			m.message = fmt.Sprintf("Zoomed to %dx", m.zoomLevel)
		} else {
			m.message = "Max zoom reached"
		}
	case "zoomOut":
		if m.zoomLevel > 1 {
			m.zoomLevel--
			m.message = fmt.Sprintf("Zoomed to %dx", m.zoomLevel)
		} else {
			m.message = "Min zoom reached"
		}
	case "brush0":
		m.selectedChar = brushSets[m.activeBrushSet].Brushes[0]
	case "brush1":
		m.selectedChar = brushSets[m.activeBrushSet].Brushes[1]
	case "brush2":
		m.selectedChar = brushSets[m.activeBrushSet].Brushes[2]
	case "brush3":
		m.selectedChar = brushSets[m.activeBrushSet].Brushes[3]
	case "erase":
		m.selectedChar = " "
	case "expTop":
		m = m.expandCanvas("top")
	case "expBot":
		m = m.expandCanvas("bottom")
	case "expLeft":
		m = m.expandCanvas("left")
	case "expRight":
		m = m.expandCanvas("right")
	case "shkTop":
		m = m.shrinkCanvas("top")
	case "shkBot":
		m = m.shrinkCanvas("bottom")
	case "shkLeft":
		m = m.shrinkCanvas("left")
	case "shkRight":
		m = m.shrinkCanvas("right")
	}

	return m
}

func (m model) getSidebarButtons() []SidebarButton {
	return []SidebarButton{
		{0, 0, 37, "   ┏┓ ╻  ┏━┓┏━╸╻┏ ┏━┓╻ ╻┏━┓╺┳┓┏━╸", "", "#D700D7"},
		{0, 1, 37, "   ┣┻┓┃  ┃ ┃┃  ┣┻┓┗━┓┣━┫┣━┫ ┃┃┣╸ ", "", "#D700D7"},
		{0, 2, 37, "   ┗━┛┗━╸┗━┛┗━╸╹ ╹┗━┛╹ ╹╹ ╹╺┻┛┗━╸", "", "#D700D7"},

		{0, 3, 14, "", "new", ""},
		{18, 3, 3, "", "focusX", ""},
		{26, 3, 3, "", "focusY", ""},

		{0, 4, 37, strings.Repeat("─", 37), "", "#444444"},

		{0, 5, 9, "   Copy", "copy", "#0087AF"},
		{9, 5, 9, "   Paste", "paste", "#0087AF"},
		{18, 5, 9, "   Undo", "undo", "#D700D7"},
		{27, 5, 10, "   Redo", "redo", "#D700D7"},

		{0, 6, 18, "       Zoom In", "zoomIn", "#5F8700"},
		{18, 6, 19, "      Zoom Out", "zoomOut", "#5F8700"},

		{0, 7, 37, strings.Repeat("─", 37), "", "#444444"},

		{0, 8, 37, "  Set: " + brushSets[m.activeBrushSet].Name, "set", "#8700AF"},

		{0, 9, 7, "   " + brushSets[m.activeBrushSet].Brushes[0], "brush0", "#FFFFFF"},
		{7, 9, 7, "   " + brushSets[m.activeBrushSet].Brushes[1], "brush1", "#FFFFFF"},
		{14, 9, 7, "   " + brushSets[m.activeBrushSet].Brushes[2], "brush2", "#FFFFFF"},
		{21, 9, 7, "   " + brushSets[m.activeBrushSet].Brushes[3], "brush3", "#FFFFFF"},
		{28, 9, 9, "  Erase", "erase", "#FFFFFF"},

		{0, 10, 37, strings.Repeat("─", 37), "", "#444444"},

		{0, 11, 37, " Expand Canvas:", "", "#888888"},
		{15, 12, 7, " [+Top]", "expTop", "#D75F00"},
		{7, 13, 7, " [+Lft]", "expLeft", "#D75F00"},
		{23, 13, 7, " [+Rgt]", "expRight", "#D75F00"},
		{15, 14, 7, " [+Bot]", "expBot", "#D75F00"},

		{0, 15, 37, " Shrink Canvas:", "", "#888888"},
		{15, 16, 7, " [-Top]", "shkTop", "#0087D7"},
		{7, 17, 7, " [-Lft]", "shkLeft", "#0087D7"},
		{23, 17, 7, " [-Rgt]", "shkRight", "#0087D7"},
		{15, 18, 7, " [-Bot]", "shkBot", "#0087D7"},

		{0, 19, 37, strings.Repeat("─", 37), "", "#444444"},

		{0, 20, 37, " Replace Mode:", "", "#888888"},
		{0, 21, 10, "  F:" + allChars[m.replaceFromIdx], "cycleFrom", "#00AFAF"},
		{10, 21, 10, "  T:" + allChars[m.replaceToIdx], "cycleTo", "#00AFAF"},
		{20, 21, 17, "    [Replace]", "execReplace", "#D70000"},

		{0, 22, 9, " Chance: ", "", "#888888"},
		{9, 22, 6, "  [<]", "chanceDec", "#00AFAF"},
		{15, 22, 14, fmt.Sprintf("     %3d%%", m.replaceChance), "", "#FFFFFF"},
		{29, 22, 8, " [>]", "chanceInc", "#00AFAF"},

		{0, 23, 5, " Pat:", "", "#888888"},
		{5, 23, 10, "   [Rnd]", "patRnd", "#555555"},
		{15, 23, 11, "    [Cls]", "patCls", "#555555"},
		{26, 23, 11, "    [Org]", "patOrg", "#555555"},
	}
}

// ── Canvas Manipulation ─────────────────────────────── ⊃

func (m model) expandCanvas(dir string) model {
	m = m.saveState(true)
	newW := m.canvasWidth
	newH := m.canvasHeight

	switch dir {
	case "top", "bottom":
		newH++
	case "left", "right":
		newW++
	}

	newCanvas := make([][]string, newH)
	for i := range newCanvas {
		newCanvas[i] = make([]string, newW)
		for j := range newCanvas[i] {
			newCanvas[i][j] = " "
		}
	}

	startY := 0
	startX := 0
	if dir == "top" {
		startY = 1
	}
	if dir == "left" {
		startX = 1
	}

	for y := 0; y < m.canvasHeight; y++ {
		for x := 0; x < m.canvasWidth; x++ {
			newCanvas[y+startY][x+startX] = m.canvas[y][x]
		}
	}

	m.canvas = newCanvas
	m.canvasWidth = newW
	m.canvasHeight = newH
	m.message = fmt.Sprintf("Expanded %s", dir)
	return m
}

func (m model) shrinkCanvas(dir string) model {
	if m.canvasWidth <= 1 && (dir == "left" || dir == "right") {
		m.message = "Too small!"
		return m
	}
	if m.canvasHeight <= 1 && (dir == "top" || dir == "bottom") {
		m.message = "Too small!"
		return m
	}

	m = m.saveState(true)
	newW := m.canvasWidth
	newH := m.canvasHeight

	switch dir {
	case "top", "bottom":
		newH--
	case "left", "right":
		newW--
	}

	newCanvas := make([][]string, newH)
	for i := range newCanvas {
		newCanvas[i] = make([]string, newW)
		for j := range newCanvas[i] {
			newCanvas[i][j] = " "
		}
	}

	srcY := 0
	srcX := 0
	if dir == "top" {
		srcY = 1
	}
	if dir == "left" {
		srcX = 1
	}

	for y := 0; y < newH; y++ {
		for x := 0; x < newW; x++ {
			newCanvas[y][x] = m.canvas[y+srcY][x+srcX]
		}
	}

	m.canvas = newCanvas
	m.canvasWidth = newW
	m.canvasHeight = newH
	m.message = fmt.Sprintf("Shrunk %s", dir)
	return m
}

func (m model) initCanvas(w, h int) model {
	m.canvasWidth = w
	m.canvasHeight = h
	m.canvas = make([][]string, h)
	for i := range m.canvas {
		m.canvas[i] = make([]string, w)
		for j := range m.canvas[i] {
			m.canvas[i][j] = " "
		}
	}
	return m
}

func (m model) executeReplace() model {
	fromChar := allChars[m.replaceFromIdx]
	toChar := allChars[m.replaceToIdx]
	if fromChar == toChar {
		m.message = "Same chars!"
		return m
	}

	m = m.saveState(true)
	
	replacedCount := 0
	
	var noiseMap [][]float64
	if m.replacePattern == "cluster" {
		noiseMap = generateClusterMap(m.canvasWidth, m.canvasHeight)
	}

	for y := 0; y < m.canvasHeight; y++ {
		for x := 0; x < m.canvasWidth; x++ {
			if m.canvas[y][x] == fromChar {
				replace := false
				
				if m.replaceChance >= 100 {
					replace = true
				} else {
					chanceRatio := float64(m.replaceChance) / 100.0
					
					switch m.replacePattern {
					case "random":
						if rand.Float64() < chanceRatio {
							replace = true
						}
					case "organic":
						dot := float64(x)*0.06711056 + float64(y)*0.00583715
						_, fractDot := math.Modf(dot)
						_, threshold := math.Modf(52.9829189 * fractDot)
						if threshold < 0 {
							threshold += 1.0
						}
						if chanceRatio > threshold {
							replace = true
						}
					case "cluster":
						if noiseMap[y][x] < chanceRatio {
							replace = true
						}
					}
				}
				
				if replace {
					m.canvas[y][x] = toChar
					replacedCount++
				}
			}
		}
	}
	m.message = fmt.Sprintf("Replaced %d chars", replacedCount)
	return m
}

// ── Clipboard Operations ─────────────────────────────── ⊃

func (m model) copyToClipboard() model {
	var sb strings.Builder
	for _, row := range m.canvas {
		sb.WriteString(strings.Join(row, ""))
		sb.WriteString("\n")
	}
	err := clipboard.WriteAll(sb.String())
	if err != nil {
		m.message = "Copy failed!"
	} else {
		m.message = "Copied!"
	}
	return m
}

func (m model) pasteFromClipboard() model {
	content, err := clipboard.ReadAll()
	if err != nil {
		m.message = "Paste failed!"
		return m
	}
	content = strings.ReplaceAll(content, "\r", "")
	content = strings.ReplaceAll(content, "\t", "    ")
	lines := strings.Split(content, "\n")

	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	h := len(lines)
	w := 0
	for _, line := range lines {
		runes := []rune(line)
		if len(runes) > w {
			w = len(runes)
		}
	}

	if w == 0 || h == 0 {
		m.message = "Clipboard empty!"
		return m
	}

	if w > 500 {
		w = 500
	}
	if h > 500 {
		h = 500
	}

	m = m.saveState(true)

	m = m.initCanvas(w, h)
	m.widthInput.SetValue(fmt.Sprintf("%d", w))
	m.heightInput.SetValue(fmt.Sprintf("%d", h))

	for y, line := range lines {
		if y >= h {
			break
		}
		runes := []rune(line)
		for x, r := range runes {
			if x < w {
				m.canvas[y][x] = string(r)
			}
		}
	}

	m.message = fmt.Sprintf("Pasted %dx%d!", w, h)
	return m
}

// ── Timeline (Undo/Redo) ─────────────────────────────── ⊃

func (m model) saveState(force bool) model {
	now := time.Now()
	if force || time.Since(m.lastSaveTime) > 200*time.Millisecond {
		copyCanvas := make([][]string, m.canvasHeight)
		for i := range m.canvas {
			copyCanvas[i] = make([]string, m.canvasWidth)
			copy(copyCanvas[i], m.canvas[i])
		}
		m.history = append(m.history, copyCanvas)
		if len(m.history) > 50 {
			m.history = m.history[1:]
		}
		m.lastSaveTime = now
		m.redoStack = m.redoStack[:0]
	}
	return m
}

func (m model) undo() model {
	if len(m.history) > 0 {
		currentCanvas := make([][]string, m.canvasHeight)
		for i := range m.canvas {
			currentCanvas[i] = make([]string, m.canvasWidth)
			copy(currentCanvas[i], m.canvas[i])
		}
		m.redoStack = append(m.redoStack, currentCanvas)

		lastState := m.history[len(m.history)-1]
		m.history = m.history[:len(m.history)-1]

		m.canvasHeight = len(lastState)
		if m.canvasHeight > 0 {
			m.canvasWidth = len(lastState[0])
		} else {
			m.canvasWidth = 0
		}
		m.canvas = lastState

		m.lastSaveTime = time.Time{}
		m.message = "Undo!"
	} else {
		m.message = "Nothing to undo"
	}
	return m
}

func (m model) redo() model {
	if len(m.redoStack) > 0 {
		currentCanvas := make([][]string, m.canvasHeight)
		for i := range m.canvas {
			currentCanvas[i] = make([]string, m.canvasWidth)
			copy(currentCanvas[i], m.canvas[i])
		}
		m.history = append(m.history, currentCanvas)

		nextState := m.redoStack[len(m.redoStack)-1]
		m.redoStack = m.redoStack[:len(m.redoStack)-1]

		m.canvasHeight = len(nextState)
		if m.canvasHeight > 0 {
			m.canvasWidth = len(nextState[0])
		} else {
			m.canvasWidth = 0
		}
		m.canvas = nextState

		m.lastSaveTime = time.Time{}
		m.message = "Redo!"
	} else {
		m.message = "Nothing to redo"
	}
	return m
}

// ── Math & Algorithmic Helpers ─────────────────────────────── ⊃

func getZoomedChar(c string, subX, subY, zoomLevel int) string {
	if c == "\u2591" || c == "\u2592" || c == "\u2593" || c == " " { // ░, ▒, ▓
		return c
	}
	var g [4]bool
	switch c {
	case "\u2588": g = [4]bool{true, true, true, true}       // █
	case "\u2580": g = [4]bool{true, true, false, false}     // ▀
	case "\u2584": g = [4]bool{false, false, true, true}     // ▄
	case "\u258c": g = [4]bool{true, false, true, false}     // ▌
	case "\u2590": g = [4]bool{false, true, false, true}     // ▐
	case "\u2596": g = [4]bool{false, false, true, false}    // ▖
	case "\u2597": g = [4]bool{false, false, false, true}    // ▗
	case "\u2598": g = [4]bool{true, false, false, false}    // ▘
	case "\u259d": g = [4]bool{false, true, false, false}    // ▝
	default: return c
	}
	x0 := (2 * subX) / zoomLevel
	x1 := (2 * subX + 1) / zoomLevel
	y0 := (2 * subY) / zoomLevel
	y1 := (2 * subY + 1) / zoomLevel
	val00 := g[y0*2+x0]
	val10 := g[y0*2+x1]
	val01 := g[y1*2+x0]
	val11 := g[y1*2+x1]
	res := [4]bool{val00, val10, val01, val11}
	switch res {
	case [4]bool{true, true, true, true}: return "\u2588"       // █
	case [4]bool{true, true, false, false}: return "\u2580"     // ▀
	case [4]bool{false, false, true, true}: return "\u2584"     // ▄
	case [4]bool{true, false, true, false}: return "\u258c"     // ▌
	case [4]bool{false, true, false, true}: return "\u2590"     // ▐
	case [4]bool{false, false, true, false}: return "\u2596"    // ▖
	case [4]bool{false, false, false, true}: return "\u2597"    // ▗
	case [4]bool{true, false, false, false}: return "\u2598"    // ▘
	case [4]bool{false, true, false, false}: return "\u259d"    // ▝
	default: return " "
	}
}

func generateClusterMap(w, h int) [][]float64 {
	gridW := w/4 + 2
	gridH := h/4 + 2
	grid := make([][]float64, gridH)
	for i := range grid {
		grid[i] = make([]float64, gridW)
		for j := range grid[i] {
			grid[i][j] = rand.Float64()
		}
	}
	
	noiseMap := make([][]float64, h)
	for y := 0; y < h; y++ {
		noiseMap[y] = make([]float64, w)
		for x := 0; x < w; x++ {
			gx := float64(x) / 4.0
			gy := float64(y) / 4.0
			ix := int(gx)
			iy := int(gy)
			fx := gx - float64(ix)
			fy := gy - float64(iy)
			
			v00 := grid[iy][ix]
			v10 := grid[iy][ix+1]
			v01 := grid[iy+1][ix]
			v11 := grid[iy+1][ix+1]
			
			top := v00*(1-fx) + v10*fx
			bot := v01*(1-fx) + v11*fx
			val := top*(1-fy) + bot*fy
			
			noiseMap[y][x] = val
		}
	}
	return noiseMap
}
