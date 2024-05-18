package main

import (
	"fmt"
	"log"
	"strings"
	"time"
	"os"
	"context"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/joho/godotenv"
	openai "github.com/sashabaranov/go-openai"
)

func main() {
	model := initialModel()
	program := tea.NewProgram(model, tea.WithMouseCellMotion())	

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

	senderColor = "#cda9d6"
	receiverColor = "#b7e4cf"

	viewportPadding = 1
	viewportWidth = 60 + 2 * viewportPadding
	viewportHeight = 20

	textareaWidth = 60
	textareaHeight = 1

)

var (
	spinnerType = spinner.MiniDot
	client *openai.Client
	ctx context.Context
	chatMessages []openai.ChatCompletionMessage
)

func initializeClient() {

	client = openai.NewClient(getApiKey())
	ctx = context.Background()
}

type model struct {
	viewport 		viewport.Model
	messages 		[]string
	rawMessages 	[]string
	textarea 		textarea.Model
	senderStyle 	lipgloss.Style
	receiverStyle 	lipgloss.Style
	spinner 		spinner.Model
	waiting 		bool
	err 			error
}

type responseMsg struct {
	message 	string
	err 		error
}

func initialModel() model {
	// Text area
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

	// Viewport
	vp := viewport.New(viewportWidth, viewportHeight)
	vp.Style = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).PaddingLeft(1)
	vp.Style.Background(lipgloss.Color(backgroundColor))

	vp.MouseWheelEnabled = true

	return model{
		viewport:		vp,
		messages: 		[]string{},
		rawMessages: 	[]string{},
		textarea: 		ta,
		senderStyle: 	lipgloss.NewStyle().Foreground(lipgloss.Color(senderColor)),
		receiverStyle: 	lipgloss.NewStyle().Foreground(lipgloss.Color(receiverColor)),
		spinner: 		spinner.New(spinner.WithSpinner(spinnerType)),
		waiting: 		false,
		err: 			nil,
	}

}

func (m model) Init() tea.Cmd {
	return textarea.Blink
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
		switch msg.Type {
			case tea.KeyCtrlC, tea.KeyEsc:
				fmt.Println(m.textarea.Value())
				return m, tea.Quit
			case tea.KeyEnter:
				message := m.textarea.Value()
				m.rawMessages = append(m.rawMessages, message)
				m.messages = append(m.messages, m.senderStyle.Render("> ") + message)

				m.messages = append(m.messages, m.receiverStyle.Render("> ") + m.spinner.View())

				UpdateViewport(&m)

				m.textarea.Reset()
				m.viewport.GotoBottom()

				m.waiting = true

				return m, tea.Batch(m.spinner.Tick, GetResponseCmd(message))
		}
	
	case spinner.TickMsg:

		if !m.waiting {
			return m, nil
		}

		m.spinner, _ = m.spinner.Update(msg)

		updatedMessage := m.receiverStyle.Render("> ") + m.spinner.View()
		m.messages = append(m.messages[:len(m.messages) - 1], updatedMessage)

		UpdateViewport(&m)

		m.textarea.Reset()
		m.viewport.GotoBottom()	
		
		// Control spinner animation
		time.Sleep(100 * time.Millisecond)

		return m, tea.Batch(m.spinner.Tick, textInputCmd, viewportCmd)

	case responseMsg:

		m.waiting = false

		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}

		m.rawMessages = append(m.rawMessages, msg.message)
		m.messages = append(m.messages[:len(m.messages) - 1], m.receiverStyle.Render("> ") + msg.message)

		UpdateViewport(&m)

		m.viewport.GotoBottom()

		return m, nil

	case error: 
		m.err = msg
		return m, nil

	}

	return m, tea.Batch(textInputCmd, viewportCmd, spinnerCmd)
}

func UpdateViewport(m *model) {
	joinedMessages := strings.Join(m.messages, "\n")

	// TODO: Handle multiline inputs correctly
	nbrLines := strings.Count(joinedMessages, "\n") + 1

	// To make chat start from bottom
	offset := strings.Repeat("\n", max(viewportHeight - nbrLines - 2, 0))

	m.viewport.SetContent(offset + joinedMessages)
}

func GetResponseCmd(message string) tea.Cmd {
    return func() tea.Msg {
		chatMessages = append(chatMessages, openai.ChatCompletionMessage{
			Role: openai.ChatMessageRoleUser,
			Content: message,
		})

		req := openai.ChatCompletionRequest{
			Model: openai.GPT3Dot5Turbo,
			Messages: chatMessages,
		}

		resp, err := client.CreateChatCompletion(ctx, req)

		return responseMsg{
			message:	resp.Choices[0].Message.Content,
			err: 		err, 
		}
	}

}

func (m *model) resetSpinner() {
	m.spinner = spinner.New()
	m.spinner.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF00FF"))
	m.spinner.Spinner = spinnerType
}

func (m model) View() string {
	return fmt.Sprintf(
			"%s\n\n%s",
			m.viewport.View(),
			m.textarea.View(),
		) + "\n\n"
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
