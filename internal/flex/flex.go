package flex

import (
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

type Box = ui.Box
type Field = ui.Field
type Look = ui.Look
type SelectState = ui.SelectState
type SliderState = ui.SliderState
type SplitterState = ui.SplitterState
type ScrollState = ui.ScrollState
type Align = ui.Align
type DialogSpec = ui.DialogSpec

const (
	Auto     = ui.Auto
	Start    = ui.Start
	Center   = ui.Center
	End      = ui.End
	Baseline = ui.Baseline
	Stretch  = ui.Stretch
	Between  = ui.Between
)

type indexOption struct {
	index int
	label string
}

func indexOptionLabel(option indexOption) string { return option.label }

type indexBinding struct {
	ready   bool
	source  int
	value   indexOption
	options []indexOption
}

func (binding *indexBinding) set(labels []string, selected int) {
	optionsChanged := len(binding.options) != len(labels)
	if !optionsChanged {
		for index, label := range labels {
			if binding.options[index].label != label {
				optionsChanged = true
				break
			}
		}
	}
	if optionsChanged {
		binding.options = binding.options[:0]
		for index, label := range labels {
			binding.options = append(binding.options, indexOption{index: index, label: label})
		}
	}
	if !binding.ready || selected != binding.source {
		binding.value = indexOption{index: selected}
		if selected >= 0 && selected < len(binding.options) {
			binding.value = binding.options[selected]
		}
		binding.source, binding.ready = selected, true
		return
	}
	if binding.value.index >= 0 && binding.value.index < len(binding.options) {
		// Keep a user's choice visible until the model emits the same value.
		binding.value.label = binding.options[binding.value.index].label
	}
}

// Frame bridges the Viva Page Ctx to GoIM's existing declarative frame calls.
type Frame struct {
	c    ui.C
	view *View
}

func (f *Frame) C() ui.C             { return f.c }
func (f *Frame) Canvas() *gfx.Canvas { return f.c.Canvas() }
func (f *Frame) Bounds() gfx.Rect    { return f.c.Bounds() }

func (f *Frame) Row() ui.Box                             { return ui.Row(f.c) }
func (f *Frame) Column() ui.Box                          { return ui.Column(f.c) }
func (f *Frame) Stack() ui.Box                           { return ui.Stack(f.c) }
func (f *Frame) Slot() ui.Box                            { return ui.Slot(f.c) }
func (f *Frame) Spacer() ui.Box                          { return ui.Spacer(f.c) }
func (f *Frame) Divider() ui.Box                         { return ui.Divider(f.c) }
func (f *Frame) Text(v string) ui.Box                    { return ui.Text(f.c, v) }
func (f *Frame) Paragraph(v string, maxLines int) ui.Box { return ui.Paragraph(f.c, v, maxLines) }
func (f *Frame) Wrap(v string) ui.Box                    { return ui.Wrap(f.c, v) }
func (f *Frame) Icon(icon gfx.Icon) ui.Box               { return ui.Icon(f.c, icon) }
func (f *Frame) Image(b gfx.Bitmap) ui.Box               { return ui.Image(f.c, b) }
func (f *Frame) Progress(fraction float32) ui.Box        { return ui.Progress(f.c, fraction) }
func (f *Frame) Spinner() ui.Box                         { return ui.Spinner(f.c) }
func (f *Frame) Button(id gfx.ID, label string) ui.Box {
	return ui.Button(f.c, label).Hit(id, label)
}
func (f *Frame) Checkbox(id gfx.ID, checked bool) ui.Box {
	return f.toggle(id, checked, false)
}
func (f *Frame) Switch(id gfx.ID, checked bool) ui.Box {
	return f.toggle(id, checked, true)
}
func (f *Frame) toggle(id gfx.ID, checked, switchControl bool) ui.Box {
	if f.view == nil {
		panic("flex: toggle requires a View")
	}
	if f.view.toggles == nil {
		f.view.toggles = make(map[gfx.ID]*bool)
	}
	value := f.view.toggles[id]
	if value == nil {
		value = new(bool)
		f.view.toggles[id] = value
	}
	*value = checked
	var box ui.Box
	if switchControl {
		box = ui.Switch(f.c, value, "")
	} else {
		box = ui.Checkbox(f.c, value, "")
	}
	return box.Hit(id, "")
}
func (f *Frame) Field(field *ui.Field, id gfx.ID, label, placeholder string) ui.Box {
	return ui.TextFieldID(f.c, id, field, label, placeholder)
}
func (f *Frame) TextAreaBase(field *ui.Field, label, placeholder string) ui.Box {
	return ui.TextArea(f.c, field, label, placeholder)
}

// Select keeps the former frame signature while binding the control to Viva's
// current typed Select. state is the stable model location that owns the open
// list between paints.
func (f *Frame) Select(scope gfx.Scope, state *ui.SelectState, labels []string, selected int) ui.Box {
	if f.view == nil {
		panic("flex: Select requires a View")
	}
	if f.view.selects == nil {
		f.view.selects = make(map[*ui.SelectState]*indexBinding)
	}
	binding := f.view.selects[state]
	if binding == nil {
		binding = &indexBinding{source: -1, value: indexOption{index: -1}}
		f.view.selects[state] = binding
	}
	binding.set(labels, selected)
	return ui.Select(f.c, &binding.value, binding.options, indexOptionLabel)
}

func (f *Frame) Tabs(scope gfx.Scope, labels []string, selected int) ui.Box {
	if f.view == nil {
		panic("flex: Tabs requires a View")
	}
	if f.view.tabs == nil {
		f.view.tabs = make(map[gfx.Scope]*indexBinding)
	}
	binding := f.view.tabs[scope]
	if binding == nil {
		binding = &indexBinding{source: -1, value: indexOption{index: -1}}
		f.view.tabs[scope] = binding
	}
	binding.set(labels, selected)
	return ui.Tabs(f.c, &binding.value, binding.options, indexOptionLabel)
}

func (f *Frame) Choice(scope gfx.Scope, labels []string, selected int) ui.Box {
	if f.view == nil {
		panic("flex: Choice requires a View")
	}
	if f.view.choices == nil {
		f.view.choices = make(map[gfx.Scope]*indexBinding)
	}
	binding := f.view.choices[scope]
	if binding == nil {
		binding = &indexBinding{source: -1, value: indexOption{index: -1}}
		f.view.choices[scope] = binding
	}
	binding.set(labels, selected)
	return ui.Choice(f.c, &binding.value, binding.options, indexOptionLabel)
}

func (f *Frame) Slider(id gfx.ID, state *ui.SliderState) ui.Box {
	return ui.Slider(f.c, state)
}

func (f *Frame) Splitter(id gfx.ID, state *ui.SplitterState) ui.Box {
	return ui.Splitter(f.c, state)
}

func (f *Frame) ScrollArea(id gfx.ID) ui.Box {
	area := ui.Scroll(f.c)
	if f.view != nil {
		if f.view.scrolls == nil {
			f.view.scrolls = make(map[gfx.ID]*ui.ScrollState)
		}
		f.view.scrolls[id] = area.ScrollState()
	}
	return area
}

func (f *Frame) Dialog(spec ui.DialogSpec) ui.Box {
	return ui.Dialog(f.c, spec)
}

// View keeps the Page and cross-paint state for one region of the main canvas.
// Each View needs a distinct Root because Viva scopes every control by the root
// of the Page that declares it.
type View struct {
	page    *ui.Page
	root    string
	theme   ui.Theme
	scrolls map[gfx.ID]*ui.ScrollState
	selects map[*ui.SelectState]*indexBinding
	tabs    map[gfx.Scope]*indexBinding
	choices map[gfx.Scope]*indexBinding
	toggles map[gfx.ID]*bool
}

// NewView names the Page scope a region declares. Use a stable, unique name for
// every View painted into the same canvas.
func NewView(root string) View { return View{root: root} }

func (v *View) Draw(canvas *gfx.Canvas, theme gfx.Theme, bounds gfx.Rect, fn func(f *Frame)) {
	v.theme = theme
	if v.page == nil {
		v.page = ui.NewPage(ui.PageConfig{
			Root:  v.root,
			Theme: &v.theme,
		})
	}
	v.page.Draw(canvas, bounds, func(c ui.C) {
		fn(&Frame{c: c, view: v})
	})
}

func (v *View) ScrollState(id gfx.ID) *ui.ScrollState {
	if v == nil || v.scrolls == nil {
		return nil
	}
	return v.scrolls[id]
}

func (v *View) Wheel(event gfx.ScrollEvent) gfx.EventResult {
	if v == nil || v.page == nil {
		return gfx.EventResult{}
	}
	return v.page.Wheel(event)
}

func (v *View) Key(event gfx.KeyEvent) gfx.EventResult {
	if v == nil || v.page == nil {
		return gfx.EventResult{}
	}
	return v.page.Key(event)
}

func (v *View) TextInput(context app.TextInputContext) *app.TextInput {
	if v == nil || v.page == nil {
		return nil
	}
	return v.page.TextInput(context)
}

func Fields(fields ...*ui.Field) app.TextInputProvider {
	return ui.Fields(fields...)
}
