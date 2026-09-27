# BlockShade Pixel Art Editor - Architecture & Flow

## 1. Introduction
BlockShade is a TUI (Terminal User Interface) pixel art editor built in Go using the `charmbracelet/bubbletea` framework. It allows users to paint using ANSI block and shading characters (░▒▓██) with full mouse support, dynamic canvas resizing, zooming, and procedural texture replacement.

## 2. Project Structure
The entire application logic is contained within `main.go` to keep the deployment simple as a single binary.

### Key Types & Data
- **`BrushSet` & `brushSets`**: Define groups of drawing characters (IBM Basic, IBM Ext, Blocks).
- **`SidebarButton`**: Defines clickable UI regions in the left toolbar (X, Y, Width, Action, Color).
- **`model`**: The central Bubble Tea state struct. It holds:
  - Terminal dimensions (`termWidth`, `termHeight`)
  - The Canvas data (`canvas [][]string`)
  - State modifiers (`zoomLevel`, `selectedChar`, `activeBrushSet`)
  - History tracking (`history`, `redoStack`) for Undo/Redo logic
  - Replace mode parameters (`replaceFromIdx`, `replaceToIdx`, `replaceChance`, `replacePattern`)

## 3. Core Data Structures
- **Canvas:** The drawing surface is a 2D slice (`[][]string`). A single string represents one cell on the grid.
- **Zoom Level:** An integer multiplier. The underlying canvas data `[][]string` never changes size when zooming; only the **rendering view** scales up dynamically.

## 4. Execution Flow (The Elm Architecture)
Bubble Tea applications follow the Model, Update, View paradigm.

### A. Initialization (`main` & `initialModel`)
1. `initialModel()` sets up the default 40x12 canvas, text inputs for custom dimensions, and default brush sets.
2. `main()` initializes the `tea.Program` with the `WithAltScreen()` (to hide previous terminal history) and `WithMouseAllMotion()` (to track mouse drags).

### B. Event Handling (`Update`)
The `Update` function receives `tea.Msg` events and updates the `model` accordingly.
- **Keyboard (`tea.KeyMsg`)**: Handles text input for canvas dimensions, and intercepts `Ctrl+Z` (Undo), `Ctrl+Y` (Redo), and `Ctrl+C / Esc` (Quit).
- **Mouse (`tea.MouseMsg`)**:
  - Checks if the mouse `X` coordinate is within the Toolbar (left sidebar) or the Canvas area.
  - **Toolbar Clicks**: Iterates over `getSidebarButtons()`. If a click hits a button's boundaries, it triggers `handleSidebarAction()` to modify state (change brush, expand canvas, copy/paste).
  - **Canvas Drags**: Translates terminal `(X,Y)` coordinates into logical `canvas[y][x]` coordinates using the current `zoomLevel`. Applies the `selectedChar` to the grid array.
  - **Mouse Wheel**: Used for cycling through brushes or zooming in/out.

### C. Rendering (`View`)
The `View` function is called after every state change to redraw the screen. It is completely stateless.
1. **Toolbar Rendering**: Loops through terminal rows `0` to `m.termHeight`. Maps `SidebarButton` definitions to lines and applies colors using `lipgloss`. If a button is active, its styling is inverted (e.g., white text on a bright background).
2. **Canvas Rendering**: Calculates the visible bounds (`viewW`, `viewH`). It maps each screen cell back to the original `m.canvas` array. 
3. **Sub-pixel Scaling**: If `zoomLevel > 1`, the view engine routes characters through `getZoomedChar()`.
4. **Assembly**: It joins the Toolbar strings with the Canvas strings side-by-side using `strings.Builder`. It uses `ansi.Truncate()` to ensure lines don't wrap and break the terminal layout.

## 5. Key Sub-Systems
- **Sub-Pixel Geometry Scaling (`getZoomedChar`)**: Instead of simply repeating characters when zoomed (which creates ugly striping), geometric block characters (like `▀`, `▌`) are mathematically mapped to a 2x2 grid. The engine calculates the exact high-res quadrant the current screen cell falls into, ensuring that shapes preserve their proportions accurately at any zoom level.
- **Organic Replacement Mode (`executeReplace`)**: Replaces characters across the canvas based on probabilities. It features 3 noise modes:
  - *Random*: Uniform random noise.
  - *Organic*: Uses an Interleaved Gradient Noise formula to create a visually pleasing "film grain" pattern.
  - *Cluster*: Generates contiguous islands of characters using procedural Bilinear Noise.
- **Timeline (Undo/Redo)**: Every mouse click or canvas action triggers `saveState()`. It deeply copies the 2D canvas array and appends it to a history slice. The stack is capped at 50 steps to prevent massive memory usage.
