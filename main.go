package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/joho/godotenv"
	"github.com/muesli/reflow/wordwrap"
	openai "github.com/sashabaranov/go-openai"
)

func main() {
	model := initialModel()
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())

	initializeClient()

	model.resetSpinner()

	if _, err := program.Run(); err != nil {
		log.Fatal(err)
	}
}

func getApiKey() string {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY is not set")
	}
	return apiKey
}

const (
	// Empty string for transparent
	backgroundColor = ""

	promptColor     = "#cda9d6"
	promptTextColor = "#fcfcfc"
	promptPrefix    = "> "

	responseColor     = "#b7e4cf"
	responseTextColor = "#e2cdb5"
	responsePrefix    = "> "

	viewportPadding   = 1
	viewportTextWidth = 80
	viewportWidth     = viewportTextWidth + 2*viewportPadding
	viewportHeight    = 22

	textareaWidth  = 80
	textareaHeight = 1

	modelName = openai.GPT3Dot5Turbo
)

var (
	spinnerType       = spinner.MiniDot
	statusSpinnerType = spinner.Line

	client       *openai.Client
	ctx          context.Context
	chatMessages []openai.ChatCompletionMessage
	baseURL      string
)

func initializeClient() {
	config := openai.DefaultConfig(getApiKey())

	// Change base URL for custom OpenAI-like endpoint
	// config.BaseURL = "https://my.api.com/v1"
	baseURL = config.BaseURL
	client = openai.NewClientWithConfig(config)
	ctx = context.Background()
}

type model struct {
	header            headerModel
	viewport          viewport.Model
	messages          []string
	textarea          textarea.Model
	promptStyle       lipgloss.Style
	promptTextStyle   lipgloss.Style
	responseStyle     lipgloss.Style
	responseTextStyle lipgloss.Style
	spinner           spinner.Model
	waiting           bool
	renderer          *glamour.TermRenderer
	err               error
}

type responseMsg struct {
	message string
	err     error
}

type statusMsg struct {
	err error
}

type headerModel struct {
	modelName      string
	statusSpinner  spinner.Model
	style          lipgloss.Style
	requestDone    bool
	requestSuccess bool
}

func (h headerModel) View() string {
	var rightIcon string
	var padAmount int
	if h.requestDone {
		padAmount = 2
		if h.requestSuccess {
			rightIcon = "✔"
		} else {
			rightIcon = "✘"
		}
	} else {
		padAmount = 4
		rightIcon = h.statusSpinner.View()
	}

	middlePadding := strings.Repeat(" ", viewportWidth-len(h.modelName)-len(rightIcon)-padAmount)
	content := modelName + middlePadding + rightIcon
	return h.style.Render(content)
}

func initialModel() model {
	// Renderer
	renderer, _ := glamour.NewTermRenderer(
		glamour.WithWordWrap(0),
	)

	return model{
		header:            NewHeader(),
		viewport:          NewViewport(),
		messages:          []string{},
		textarea:          NewTextarea(),
		promptStyle:       StyleFromColor(promptColor),
		promptTextStyle:   StyleFromColor(promptTextColor),
		responseStyle:     StyleFromColor(responseColor),
		responseTextStyle: StyleFromColor(responseTextColor),
		spinner:           spinner.New(spinner.WithSpinner(spinnerType)),
		waiting:           false,
		renderer:          renderer,
		err:               nil,
	}

}

func StyleFromColor(color string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color))
}

func NewHeader() headerModel {
	headerModel := headerModel{
		modelName:     modelName,
		statusSpinner: spinner.New(spinner.WithSpinner(statusSpinnerType)),
		requestDone:   false,
	}

	border := lipgloss.RoundedBorder()
	border.Bottom = ""
	border.BottomLeft = ""
	border.BottomRight = ""

	headerStyle := lipgloss.
		NewStyle().
		Width(viewportTextWidth).
		Height(1).
		Padding(0, 1).
		Border(border, true, true, false, true).
		Foreground(lipgloss.Color("#636363"))

	headerModel.style = headerStyle

	return headerModel
}

func NewTextarea() textarea.Model {
	ta := textarea.New()
	ta.Focus()

	ta.Prompt = "┃ "
	ta.CharLimit = 280

	ta.SetWidth(textareaWidth)
	ta.SetHeight(textareaHeight)

	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()

	ta.Placeholder = "..."
	ta.ShowLineNumbers = false

	ta.KeyMap.InsertNewline.SetEnabled(false)

	// Add border
	borderStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder())

	ta.FocusedStyle.Base = borderStyle
	ta.BlurredStyle.Base = borderStyle

	return ta
}

func NewViewport() viewport.Model {
	vp := viewport.New(viewportWidth, viewportHeight+2)
	vpBorder := lipgloss.RoundedBorder()
	vpBorder.TopLeft = "├"
	vpBorder.TopRight = "┤"

	vp.Style = lipgloss.NewStyle().Border(vpBorder).PaddingLeft(1)
	vp.Style.Background(lipgloss.Color(backgroundColor))

	vp.MouseWheelEnabled = true

	// just use scrolling or arrows for scrolling
	vp.KeyMap = viewport.KeyMap{
		Up: key.NewBinding(
			key.WithKeys("up"),
			key.WithHelp("↑", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down"),
			key.WithHelp("↓", "down"),
		),
	}
	return vp
}

func (m model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, GetStatusCmd(), m.header.statusSpinner.Tick)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		textInputCmd tea.Cmd
		viewportCmd  tea.Cmd
		spinnerCmd   tea.Cmd
	)

	m.textarea, textInputCmd = m.textarea.Update(msg)
	m.viewport, viewportCmd = m.viewport.Update(msg)

	if m.waiting {
		m.spinner, spinnerCmd = m.spinner.Update(msg)
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			fmt.Println(m.textarea.Value())
			return m, tea.Quit
		case "enter":
			log.Printf("Msg: %v", msg.Type)
			log.Printf("Message: %v", m.textarea.Value())
			log.Printf("Message line count: %v", m.viewport.TotalLineCount())

			message := strings.TrimSpace(m.textarea.Value())
			message = wordwrap.String(message, viewportTextWidth-3)

			m.messages = append(m.messages, m.promptStyle.Render(promptPrefix)+m.promptTextStyle.Render(message))
			m.messages = append(m.messages, m.responseStyle.Render(responsePrefix)+m.spinner.View())

			UpdateViewport(&m)

			log.Printf("Viewport line count: %v\n", m.viewport.TotalLineCount())

			m.textarea.Reset()
			m.viewport.GotoBottom()

			m.waiting = true

			return m, tea.Batch(m.spinner.Tick, GetResponseCmd(message), textInputCmd, viewportCmd)

		}

	case spinner.TickMsg:

		if msg.ID == m.spinner.ID() {
			if !m.waiting {
				return m, nil
			}

			m.spinner, _ = m.spinner.Update(msg)

			updatedMessage := m.responseStyle.Render(responsePrefix) + m.spinner.View()
			m.messages = append(m.messages[:len(m.messages)-1], updatedMessage)

			UpdateViewport(&m)

			m.textarea.Reset()
			m.viewport.GotoBottom()

			// Control spinner animation
			time.Sleep(100 * time.Millisecond)

			return m, tea.Batch(m.spinner.Tick, textInputCmd, viewportCmd)
		} else if msg.ID == m.header.statusSpinner.ID() {
			if m.header.requestDone {
				return m, nil
			}

			m.header.statusSpinner, _ = m.header.statusSpinner.Update(msg)

			time.Sleep(100 * time.Millisecond)

			return m, tea.Batch(m.header.statusSpinner.Tick, textInputCmd, viewportCmd)

		} else {
			return m, nil
		}

	case responseMsg:
		log.Printf("Msg: %T", msg)

		m.waiting = false

		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}

		log.Printf("Original line count: %v", strings.Count(msg.message, "\n")+1)
		log.Printf("Original message: \n%v", msg.message)

		message := wordwrap.String(msg.message, viewportTextWidth-3)
		response := m.responseStyle.Render(responsePrefix) + m.responseTextStyle.Render(message)
		m.messages = append(m.messages[:len(m.messages)-1], response)

		log.Printf("Wrapped line count: %v", strings.Count(message, "\n")+1)

		UpdateViewport(&m)

		log.Printf("Viewport line count: %v\n", m.viewport.TotalLineCount())

		m.viewport.GotoBottom()

		return m, nil

	case statusMsg:
		m.header.requestDone = true

		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}

		m.header.requestSuccess = true

		return m, nil

	case error:
		log.Printf("Msg: %v", msg)
		m.err = msg
		return m, nil

	}

	return m, tea.Batch(textInputCmd, viewportCmd, spinnerCmd)
}

func UpdateViewport(m *model) {
	// TODO: Make chat start from bottom

	toDisplay := strings.Join(m.messages, "\n") + "\n\u200e"
	toDisplay, _ = m.renderer.Render(toDisplay + "\n ")

	m.viewport.SetContent(toDisplay)
}

func GetResponseCmd(message string) tea.Cmd {
	return func() tea.Msg {
		chatMessages = append(chatMessages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleUser,
			Content: message,
		})

		log.Print("Chat messages: ", chatMessages)

		req := openai.ChatCompletionRequest{
			Model:    modelName,
			Messages: chatMessages,
		}

		resp, err := client.CreateChatCompletion(ctx, req)

		chatMessages = append(chatMessages, resp.Choices[0].Message)

		message := resp.Choices[0].Message.Content

		return responseMsg{
			message: message,
			err:     err,
		}
	}

}

func GetStatusCmd() tea.Cmd {
	return func() tea.Msg {
		// make get request to the clients base url
		_, err := client.ListModels(ctx)

		return statusMsg{
			err: err,
		}
	}
}

func (m *model) resetSpinner() {
	m.spinner = spinner.New()
	m.spinner.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF00FF"))
	m.spinner.Spinner = spinnerType
}

func (m model) View() string {
	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.header.View(),
		m.viewport.View(),
		m.textarea.View(),
	)
}
