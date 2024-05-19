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

	promptColor = "#cda9d6"
	promptTextColor = "#fcfcfc"
	promptPrefix = "> "

	responseColor = "#b7e4cf"
	responseTextColor = "#e2cdb5"
	responsePrefix = "> "

	viewportPadding = 1
	viewportTextWidth = 80
	viewportWidth = viewportTextWidth + 2 * viewportPadding
	viewportHeight = 20

	textareaWidth = 80
	textareaHeight = 1

)

var (
	spinnerType = spinner.MiniDot

	client *openai.Client
	ctx context.Context
	chatMessages []openai.ChatCompletionMessage
)

func initializeClient() {
	config := openai.DefaultConfig(getApiKey())	

	// Change base URL for custom OpenAI-like endpoint
	// config.BaseURL = "https://my.api.com/v1"
	client = openai.NewClientWithConfig(config)
	ctx = context.Background()
}

type model struct {
	viewport 			viewport.Model
	messages 			[]string
	textarea 			textarea.Model
	promptStyle 		lipgloss.Style
	promptTextStyle 	lipgloss.Style
	responseStyle 		lipgloss.Style
	responseTextStyle 	lipgloss.Style
	spinner 			spinner.Model
	waiting 			bool
	err 				error
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

	// vp.HighPerformanceRendering = true

	return model{
		viewport:			vp,
		messages: 			[]string{},
		textarea: 			ta,
		promptStyle: 		lipgloss.NewStyle().Foreground(lipgloss.Color(promptColor)),
		promptTextStyle: 	lipgloss.NewStyle().Foreground(lipgloss.Color(promptTextColor)),
		responseStyle: 		lipgloss.NewStyle().Foreground(lipgloss.Color(responseColor)),
		responseTextStyle: 	lipgloss.NewStyle().Foreground(lipgloss.Color(responseTextColor)),
		spinner: 			spinner.New(spinner.WithSpinner(spinnerType)),
		waiting: 			false,
		err: 				nil,
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

				m.messages = append(m.messages, m.promptStyle.Render(promptPrefix) + m.promptTextStyle.Render(message))
				m.messages = append(m.messages, m.responseStyle.Render(responsePrefix) + m.spinner.View())

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

		updatedMessage := m.responseStyle.Render(responsePrefix) + m.spinner.View()
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

		response := m.responseStyle.Render(responsePrefix) + m.responseTextStyle.Render(msg.message)
		m.messages = append(m.messages[:len(m.messages) - 1], response)

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
	// nbrLines := strings.Count(joinedMessages, "\n") + 1

	// To make chat start from bottom
	// offset := strings.Repeat("\n", max(viewportHeight - nbrLines - 2, 0))
	offset := ""
	// toDisplay := lipgloss.NewStyle().Render(offset + joinedMessages)

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
