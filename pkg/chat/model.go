package chat

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gptscript-ai/go-gptscript"
)

type Model struct {
	History   []Run
	Run       Run
	ChatState string
	ToolPath  string
	Opt       gptscript.Opts
}

type Run struct {
	Calls  map[string]Call
	Output string
	Input  string
	Error  string
	Start  time.Time
	End    time.Time
	State  gptscript.RunState
}

type Call struct {
	gptscript.CallContext

	Start  time.Time
	End    time.Time
	Input  string
	Output string
}

func (r *Model) next(ctx context.Context, client *gptscript.Client, input string) tea.Msg {
	opts := r.Opt
	opts.IncludeEvents = true
	opts.ChatState = r.ChatState
	opts.Input = input

	if len(r.Run.Calls) != 0 {
		r.History = append(r.History, r.Run)
	}

	r.Run = Run{
		Calls: map[string]Call{},
		Input: input,
		Start: time.Now(),
		State: gptscript.Creating,
	}

	run, err := client.Run(ctx, r.ToolPath, opts)
	if err != nil {
		return &Event{
			Err: &err,
		}
	}

	event := &Event{
		Run: run,
	}
	return event.next()
}

func (r *Model) Submit(ctx context.Context, run *gptscript.Client, input string) tea.Cmd {
	if !r.Run.State.IsTerminal() {
		return func() tea.Msg {
			err := fmt.Errorf("run already running")
			return &Event{
				Err: &err,
			}
		}
	}

	return func() tea.Msg {
		return r.next(ctx, run, input)
	}
}

func (r *Model) Update(event *Event) tea.Cmd {
	if event.Err != nil {
		r.Run.Error = (*event.Err).Error()
		r.Run.State = gptscript.Error
		return nil
	}

	if event.Event != nil {
		r.process(*event.Event)
		return event.next
	}

	if event.Run != nil {
		r.Run.Output = event.Output
		r.Run.State = event.Run.State()

		switch r.Run.State {
		case gptscript.Continue:
			r.ChatState = event.Run.ChatState()
		case gptscript.Finished:
			r.ChatState = event.Run.ChatState()
		case gptscript.Error:
			r.Run.Error = event.Run.ErrorOutput()
		default:
			panic("invalid state " + event.Run.State())
		}
	}

	if event.Err != nil && r.Run.Error == "" {
		r.Run.Error = (*event.Err).Error()
		r.Run.State = gptscript.Error
	}

	if r.Run.End.IsZero() {
		r.Run.End = time.Now()
	}

	return nil
}

func (r *Model) process(event gptscript.Event) {
	if event.CallContext == nil || event.CallContext.ID == "" {
		return
	}

	call := r.Run.Calls[event.CallContext.ID]
	call.CallContext = *event.CallContext

	switch event.Type {
	case gptscript.EventTypeCallStart:
		call.Start = event.Time
		call.Input = event.Content
	case gptscript.EventTypeCallProgress:
		call.Output = event.Content
		if call.ParentID == "" {
			r.Run.Output = call.Output
		}
	case gptscript.EventTypeCallFinish:
		call.End = event.Time
		call.Output = event.Content
	}

	r.Run.Calls[event.CallContext.ID] = call
}

type Event struct {
	Run    *gptscript.Run
	Event  *gptscript.Event
	Err    *error
	Output string
}

func (r *Event) next() tea.Msg {
	event, ok := <-r.Run.Events()
	if ok {
		r.Event = &event
	} else {
		output, err := r.Run.Text()
		if err != nil {
			r.Err = &err
		}

		r.Output = output
		r.Event = nil
	}
	return r
}
