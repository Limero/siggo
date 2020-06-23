package widgets

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/derricw/siggo/model"
	"github.com/derricw/siggo/signal"
	"github.com/gdamore/tcell"
	"github.com/kyokomi/emoji"
	"github.com/rivo/tview"
	log "github.com/sirupsen/logrus"
	"github.com/skratchdot/open-golang/open"
)

type Mode int

const (
	NormalMode Mode = iota
	InsertMode
	YankMode
	OpenMode
)

// stolen from suckoverflow
var urlRegex = regexp.MustCompile(`https?:\/\/(www\.)?[-a-zA-Z0-9@:%._\+~#=]{1,256}\.[a-zA-Z0-9()]{1,6}\b([-a-zA-Z0-9()@:%_\+.~#?&//=]*)`)

type ConvInfo map[*model.Contact]*model.Conversation

// ChatWindow is the main panel for the UI.
type ChatWindow struct {
	// todo: maybe use Flex instead of Grid?
	*tview.Grid
	siggo          *model.Siggo
	currentContact *model.Contact
	mode           Mode

	sendPanel         *SendPanel
	contactsPanel     *ContactListPanel
	conversationPanel *ConversationPanel
	searchPanel       tview.Primitive
	commandPanel      tview.Primitive
	statusBar         *StatusBar
	app               *tview.Application
	normalKeybinds    func(*tcell.EventKey) *tcell.EventKey
	yankKeybinds      func(*tcell.EventKey) *tcell.EventKey
	openKeybinds      func(*tcell.EventKey) *tcell.EventKey
	goKeybinds        func(*tcell.EventKey) *tcell.EventKey
}

// InsertMode enters insert mode
func (c *ChatWindow) InsertMode() {
	log.Debug("INSERT MODE")
	c.app.SetFocus(c.sendPanel)
	c.sendPanel.SetBorderColor(tcell.ColorOrange)
	c.mode = InsertMode
}

// YankMode enters yank mode
func (c *ChatWindow) YankMode() {
	log.Debug("YANK MODE")
	c.conversationPanel.SetBorderColor(tcell.ColorOrange)
	c.mode = YankMode
	c.SetInputCapture(c.yankKeybinds)
}

// OpenMode enters open mode
func (c *ChatWindow) OpenMode() {
	log.Debug("OPEN MODE")
	c.conversationPanel.SetBorderColor(tcell.ColorBlueViolet)
	c.mode = OpenMode
	c.SetInputCapture(c.openKeybinds)
}

// NormalMode enters normal mode
func (c *ChatWindow) NormalMode() {
	log.Debug("NORMAL MODE")
	c.app.SetFocus(c)
	// clear our highlights
	c.conversationPanel.SetBorderColor(tcell.ColorWhite)
	c.sendPanel.SetBorderColor(tcell.ColorWhite)
	c.mode = NormalMode
	c.SetInputCapture(c.normalKeybinds)
}

// YankLastMsg copies the last message of a conversation to the clipboard.
func (c *ChatWindow) YankLastMsg() {
	c.NormalMode()
	conv, err := c.currentConversation()
	if err != nil {
		c.SetErrorStatus(err)
		return
	}
	if conv == nil {
		c.SetErrorStatus(fmt.Errorf("<NO CONVERSATION>")) // this shouldn't happen
		return
	}
	var lastMsg *model.Message
	if lastMsg = conv.LastMessage(); lastMsg == nil {
		c.SetStatus("📋<NO MESSAGES>") // this is fine
		return
	}
	content := strings.TrimSpace(lastMsg.Content)
	err = clipboard.WriteAll(content)
	if err != nil {
		c.SetErrorStatus(err)
		return
	}
	c.SetStatus(fmt.Sprintf("📋%s", content))
}

func (c *ChatWindow) getLinks() []string {
	toSearch := c.conversationPanel.GetText(true)
	return urlRegex.FindAllString(toSearch, -1)
}

func (c *ChatWindow) getAttachments() []*signal.Attachment {
	a := make([]*signal.Attachment, 0)
	conv, err := c.currentConversation()
	if err != nil {
		return a
	}
	// TODO: make siggo.Conversation keep a list of attachments
	// so that we don't have to search for them like this
	for _, ID := range conv.MessageOrder {
		msg := conv.Messages[ID]
		if len(msg.Attachments) > 0 {
			a = append(a, msg.Attachments...)
		}
	}
	return a
}

// YankLastLink copies the last link in a converstaion to the clipboard
func (c *ChatWindow) YankLastLink() {
	c.NormalMode()
	links := c.getLinks()
	if len(links) > 0 {
		last := links[len(links)-1]
		if err := clipboard.WriteAll(last); err != nil {
			c.SetErrorStatus(err)
			return
		}
		c.SetStatus(fmt.Sprintf("📋%s", last))
	} else {
		c.SetStatus(fmt.Sprintf("📋<NO MATCHES>"))
	}
}

// OpenLastLink opens the last link that is finds in the conversation
// TODO: solution for browsing/opening any link
func (c *ChatWindow) OpenLastLink() {
	c.NormalMode()
	links := c.getLinks()
	if len(links) > 0 {
		last := links[len(links)-1]
		err := open.Run(last)
		if err != nil {
			c.SetErrorStatus(fmt.Errorf("<OPEN FAILED: %v>", err))
		} else {
			c.SetStatus(fmt.Sprintf("📂%s", last))
		}
	} else {
		c.SetStatus(fmt.Sprintf("📂<NO MATCHES>"))
	}
}

// OpenLastAttachment opens the last attachment that it finds in the conversation
// TODO: solution for browsing/opening any attachment
func (c *ChatWindow) OpenLastAttachment() {
	c.NormalMode()
	attachments := c.getAttachments()
	if len(attachments) > 0 {
		last := attachments[len(attachments)-1]
		path, err := last.Path()
		if err != nil {
			c.SetErrorStatus(fmt.Errorf("📎failed to find attachment: %v", err))
			return
		}
		go func() {
			err = open.Run(path)
			if err != nil {
				c.SetErrorStatus(fmt.Errorf("📎<OPEN FAILED: %v>", err))
			} else {
				c.SetStatus(fmt.Sprintf("📎%s", path))
			}
		}()
	} else {
		c.SetStatus(fmt.Sprintf("📎<NO MATCHES>"))
	}
}

// ShowContactSearch opens a contact search panel
func (c *ChatWindow) ShowContactSearch() {
	log.Debug("SHOWING CONTACT SEARCH")
	p := NewContactSearch(c)
	c.searchPanel = p
	c.SetRows(0, 3, p.maxHeight)
	c.AddItem(p, 2, 0, 1, 2, 0, 0, false)
	c.app.SetFocus(p)
}

// HideSearch hides any current search panel
func (c *ChatWindow) HideSearch() {
	log.Debug("HIDING SEARCH")
	c.RemoveItem(c.searchPanel)
	c.SetRows(0, 3)
	c.app.SetFocus(c)
}

// ShowAttachInput opens a commandPanel to choose a file to attach
func (c *ChatWindow) ShowAttachInput() {
	log.Debug("SHOWING CONTACT SEARCH")
	p := NewAttachInput(c)
	c.commandPanel = p
	c.SetRows(0, 3, 1)
	c.AddItem(p, 2, 0, 1, 2, 0, 0, false)
	c.app.SetFocus(p)
}

// HideCommandInput hides any current CommandInput panel
func (c *ChatWindow) HideCommandInput() {
	log.Debug("HIDING COMMAND INPUT")
	c.RemoveItem(c.commandPanel)
	c.SetRows(0, 3)
	c.app.SetFocus(c)
}

// ShowStatusBar shows the bottom status bar
func (c *ChatWindow) ShowStatusBar() {
	c.SetRows(0, 3, 1)
	c.AddItem(c.statusBar, 2, 0, 1, 2, 0, 0, false)
}

// HideStatusBar stops showing the status bar
func (c *ChatWindow) HideStatusBar() {
	c.RemoveItem(c.statusBar) // do we actually need to do this?
	c.SetRows(0, 3)
}

// SetStatus shows a status message on the status bar
func (c *ChatWindow) SetStatus(statusMsg string) {
	log.Info(statusMsg)
	c.statusBar.SetText(statusMsg)
	c.ShowStatusBar()
}

// SetErrorStatus shows an error status in the status bar
func (c *ChatWindow) SetErrorStatus(err error) {
	log.Errorf("%s", err)
	c.statusBar.SetText(fmt.Sprintf("🔥%s", err))
	c.ShowStatusBar()
}

func (c *ChatWindow) currentConversation() (*model.Conversation, error) {
	currentConv, ok := c.siggo.Conversations()[c.currentContact]
	if ok {
		return currentConv, nil
	} else {
		return nil, fmt.Errorf("no conversation for current contact: %v", c.currentContact)
	}
}

// SetCurrentContact sets the active contact
func (c *ChatWindow) SetCurrentContact(contact *model.Contact) error {
	log.Debugf("setting current contact to: %v", contact)
	c.currentContact = contact
	c.contactsPanel.GotoContact(contact)
	c.contactsPanel.Render()
	conv, err := c.currentConversation()
	if err != nil {
		return err
	}
	c.conversationPanel.Update(conv)
	conv.CaughtUp()
	c.sendPanel.Update()
	c.conversationPanel.ScrollToEnd()
	return nil
}

// NextUnreadMessage searches for the next conversation with unread messages and makes that the
// active conversation.
func (c *ChatWindow) NextUnreadMessage() error {
	for contact, conv := range c.siggo.Conversations() {
		if conv.HasNewMessage {
			err := c.SetCurrentContact(contact)
			if err != nil {
				c.SetErrorStatus(err)
			}
		}
	}
	return nil
}

// TODO: remove code duplication with ContactDown()
func (c *ChatWindow) ContactUp() {
	log.Debug("PREVIOUS CONVERSATION")
	prevContact := c.contactsPanel.Previous()
	if prevContact != c.currentContact {
		err := c.SetCurrentContact(prevContact)
		if err != nil {
			c.SetErrorStatus(err)
		}
	}
}

func (c *ChatWindow) ContactDown() {
	log.Debug("NEXT CONVERSATION")
	nextContact := c.contactsPanel.Next()
	if nextContact != c.currentContact {
		err := c.SetCurrentContact(nextContact)
		if err != nil {
			c.SetErrorStatus(err)
		}
	}
}

// Compose opens an EDITOR to compose a command. If any text is saved in the buffer,
// we send it as a message to the current conversation.
func (c *ChatWindow) Compose() {
	msg := ""
	var err error

	success := c.app.Suspend(func() {
		msg, err = FancyCompose()
	})
	// need to sleep because there seems to be a race condition in tview
	// https://github.com/rivo/tview/issues/244
	time.Sleep(100 * time.Millisecond)
	if !success {
		c.SetErrorStatus(fmt.Errorf("failed to suspend siggo"))
		return
	}
	if err != nil {
		c.SetErrorStatus(err)
		return
	}
	if msg != "" {
		msg = emoji.Sprint(msg)
		contact := c.currentContact
		c.ShowTempSentMsg(msg)
		go c.siggo.Send(msg, contact)
		log.Infof("sending message: %s to contact: %s", msg, contact)
	}
}

// ShowTempSentMsg shows a temporary message when a message is sent but before delivery.
// Only displayed for the second or two after a message is sent.
func (c *ChatWindow) ShowTempSentMsg(msg string) {
	tmpMsg := &model.Message{
		Content:     msg,
		From:        " ~ ",
		Timestamp:   time.Now().Unix() * 1000,
		IsDelivered: false,
		IsRead:      false,
		FromSelf:    true,
	}
	// write directly to conv panel but don't add to conversation
	// no color since its from us
	c.conversationPanel.Write([]byte(tmpMsg.String("")))
}

// Quit shuts down gracefully
func (c *ChatWindow) Quit() {
	c.app.Stop()
	// do we need to do anything else?
	c.siggo.Quit()
	os.Exit(0)
}

func (c *ChatWindow) update() {
	convs := c.siggo.Conversations()
	if convs != nil {
		c.contactsPanel.Render()
		currentConv, ok := convs[c.currentContact]
		if ok {
			c.conversationPanel.Update(currentConv)
		} else {
			panic("no conversation for current contact")
		}
	}
}

type SendPanel struct {
	*tview.InputField
	parent *ChatWindow
	siggo  *model.Siggo
}

func (s *SendPanel) Send() {
	msg := s.GetText()
	contact := s.parent.currentContact
	s.parent.ShowTempSentMsg(msg)
	go s.siggo.Send(msg, contact)
	log.Infof("sent message: %s to contact: %s", msg, contact)
	s.SetText("")
	s.SetLabel("")
}

func (s *SendPanel) Defocus() {
	s.parent.NormalMode()
}

func (s *SendPanel) Update() {
	conv, err := s.parent.currentConversation()
	if err != nil {
		return
	}
	nAttachments := conv.NumAttachments()
	if nAttachments > 0 {
		s.SetLabel(fmt.Sprintf("📎(%d):", nAttachments))
	} else {
		s.SetLabel("")
	}
}

// emojify is a custom input change handler that provides emoji support
func (s *SendPanel) emojify(input string) {
	if strings.HasSuffix(input, ":") {
		//log.Printf("emojify: %s", input)
		emojified := emoji.Sprint(input)
		if emojified != input {
			s.SetText(emojified)
		}
	}
}

func NewSendPanel(parent *ChatWindow, siggo *model.Siggo) *SendPanel {
	s := &SendPanel{
		InputField: tview.NewInputField(),
		siggo:      siggo,
		parent:     parent,
	}
	s.SetTitle(" send: ")
	s.SetTitleAlign(0)
	s.SetBorder(true)
	//s.SetFieldBackgroundColor(tcell.ColorDefault)
	s.SetFieldBackgroundColor(tview.Styles.PrimitiveBackgroundColor)
	s.SetChangedFunc(s.emojify)
	s.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyESC:
			s.Defocus()
			return nil
		case tcell.KeyEnter:
			s.Send()
			return nil
		case tcell.KeyCtrlQ:
			s.parent.Quit()
		case tcell.KeyCtrlL:
			s.SetText("")
			return nil
		}
		return event
	})
	return s
}

type ContactListPanel struct {
	*tview.TextView
	siggo          *model.Siggo
	parent         *ChatWindow
	sortedContacts []*model.Contact
	currentIndex   int
}

func (cl *ContactListPanel) Next() *model.Contact {
	if cl.currentIndex < len(cl.sortedContacts)-1 {
		cl.currentIndex++
	}
	return cl.sortedContacts[cl.currentIndex]
}

func (cl *ContactListPanel) Previous() *model.Contact {
	if cl.currentIndex > 0 {
		cl.currentIndex--
	}
	return cl.sortedContacts[cl.currentIndex]
}

// GotoIndex goes to a particular contact index and return the Contact. Negative indexing is
// allowed.
func (cl *ContactListPanel) GotoIndex(index int) *model.Contact {
	if index < 0 {
		return cl.GotoIndex(len(cl.sortedContacts) - index)
	}
	if index >= len(cl.sortedContacts) {
		return cl.GotoIndex(-1)
	}
	cl.currentIndex = index
	return cl.sortedContacts[index]
}

// GotoContact goes to a particular contact.
// TODO: constant time way to do this?
func (cl *ContactListPanel) GotoContact(contact *model.Contact) {
	for i, c := range cl.sortedContacts {
		if contact == c {
			cl.GotoIndex(i)
		}
	}
}

// Render re-renders the contact list
func (cl *ContactListPanel) Render() {
	data := ""
	log.Debug("updating contact panel...")
	// this is dumb, we re-sort every update
	// TODO: don't
	sorted := cl.siggo.Contacts().SortedByIndex()
	convs := cl.siggo.Conversations()
	log.Debugf("sorted contacts: %v", sorted)
	for i, c := range sorted {
		id := c.String()
		line := fmt.Sprintf("%s\n", id)
		color := convs[c].Color()
		if cl.currentIndex == i {
			line = fmt.Sprintf("[%s::r]%s[-::-]", color, line)
			cl.currentIndex = i
		} else if convs[c].HasNewMessage {
			line = fmt.Sprintf("[%s::b]*%s[-::-]", color, line)
		} else {
			line = fmt.Sprintf("[%s::]%s[-::]", color, line)
		}
		data += line
	}
	cl.sortedContacts = sorted
	cl.SetText(data)
}

// NewContactListPanel creates a new contact list widget
func NewContactListPanel(parent *ChatWindow, siggo *model.Siggo) *ContactListPanel {
	c := &ContactListPanel{
		TextView: tview.NewTextView(),
		siggo:    siggo,
		parent:   parent,
	}
	c.SetDynamicColors(true)
	c.SetTitle("contacts")
	c.SetTitleAlign(0)
	c.SetBorder(true)
	return c
}

type ConversationPanel struct {
	*tview.TextView
	hideTitle       bool
	hidePhoneNumber bool
}

func (p *ConversationPanel) Update(conv *model.Conversation) {
	p.Clear()
	p.SetText(conv.String())
	if !p.hideTitle {
		if !p.hidePhoneNumber {
			p.SetTitle(fmt.Sprintf("%s <%s>", conv.Contact.String(), conv.Contact.Number))
		} else {
			p.SetTitle(conv.Contact.String())
		}
	}
	conv.HasNewMessage = false
}

func (p *ConversationPanel) Clear() {
	p.SetText("")
}

func NewConversationPanel(siggo *model.Siggo) *ConversationPanel {
	c := &ConversationPanel{
		TextView: tview.NewTextView(),
	}
	c.SetDynamicColors(true)
	c.SetTitle("<name of contact>")
	c.SetTitleAlign(0)
	c.SetBorder(true)
	return c
}

type SearchPanel struct {
	*tview.Grid
	list      *tview.TextView
	input     *SearchInput
	parent    *ChatWindow
	maxHeight int
}

func (p *SearchPanel) Close() {
	p.parent.HideSearch()
}

func NewContactSearch(parent *ChatWindow) *SearchPanel {
	maxHeight := 10
	p := &SearchPanel{
		Grid:      tview.NewGrid().SetRows(maxHeight-3, 1),
		list:      tview.NewTextView(),
		parent:    parent,
		maxHeight: maxHeight,
	}
	//contactList := parent.siggo.Contacts().SortedByName()
	p.input = NewSearchInput(p)
	p.AddItem(p.list, 0, 0, 1, 1, 0, 0, false)
	p.AddItem(p.input, 1, 0, 1, 1, 0, 0, true)
	p.SetBorder(true)
	p.SetTitle("search contacts...")
	return p
}

type SearchInput struct {
	*tview.InputField
	parent *SearchPanel
}

func NewSearchInput(parent *SearchPanel) *SearchInput {
	si := &SearchInput{
		InputField: tview.NewInputField(),
		parent:     parent,
	}
	si.SetLabel("> ")
	si.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// Setup keys
		log.Debugf("Key Event <SEARCH>: %v mods: %v rune: %v", event.Key(), event.Modifiers(), event.Rune())
		switch event.Key() {
		case tcell.KeyESC:
			si.parent.Close()
			return nil
		}
		return event
	})
	return si
}

// CommandInput is an input field that appears at the bottom of the window and allows for various
// commands
type CommandInput struct {
	*tview.InputField
	parent *ChatWindow
}

// AttachInput is a command input that selects an attachment and attaches it to the current
// conversation to be sent in the next message.
func NewAttachInput(parent *ChatWindow) *CommandInput {
	ci := &CommandInput{
		InputField: tview.NewInputField(),
		parent:     parent,
	}
	ci.SetLabel("📎: ")
	ci.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// Setup keys
		log.Debugf("Key Event <ATTACH>: %v mods: %v rune: %v", event.Key(), event.Modifiers(), event.Rune())
		switch event.Key() {
		case tcell.KeyESC:
			ci.parent.HideCommandInput()
			return nil
		case tcell.KeyTAB:
			// TODO: this is the part where we tab complete
			return nil
		case tcell.KeyEnter:
			path := ci.GetText()
			ci.parent.HideCommandInput()
			if path == "" {
				return nil
			}
			conv, err := ci.parent.currentConversation()
			if err != nil {
				ci.parent.SetErrorStatus(fmt.Errorf("couldn't find conversation: %v", err))
				return nil
			}
			err = conv.AddAttachment(path)
			if err != nil {
				ci.parent.SetErrorStatus(fmt.Errorf("failed to attach: %s - %v", path, err))
				return nil
			}
			ci.parent.sendPanel.Update()
			return nil
		}
		return event
	})
	return ci
}

type StatusBar struct {
	*tview.TextView
	parent *ChatWindow
}

func NewStatusBar(parent *ChatWindow) *StatusBar {
	sb := &StatusBar{
		TextView: tview.NewTextView(),
		parent:   parent,
	}
	return sb
}

func NewChatWindow(siggo *model.Siggo, app *tview.Application) *ChatWindow {
	layout := tview.NewGrid().
		SetRows(0, 3).
		SetColumns(20, 0)
	w := &ChatWindow{
		Grid:  layout,
		siggo: siggo,
		app:   app,
	}

	w.conversationPanel = NewConversationPanel(siggo)
	convInputHandler := w.conversationPanel.InputHandler()
	w.contactsPanel = NewContactListPanel(w, siggo)
	w.sendPanel = NewSendPanel(w, siggo)
	w.statusBar = NewStatusBar(w)
	// NORMAL MODE KEYBINDINGS
	w.normalKeybinds = func(event *tcell.EventKey) *tcell.EventKey {
		log.Debugf("Key Event <NORMAL>: %v mods: %v rune: %v", event.Key(), event.Modifiers(), event.Rune())
		switch event.Key() {
		case tcell.KeyRune:
			switch event.Rune() {
			case 106: // j
				convInputHandler(event, func(p tview.Primitive) {})
				return nil
			case 107: // k
				convInputHandler(event, func(p tview.Primitive) {})
				return nil
			case 74: // J
				w.ContactDown()
				return nil
			case 75: // K
				w.ContactUp()
				return nil
			case 105: // i
				w.InsertMode()
				return nil
			case 73: // I
				w.Compose()
				return nil
			case 121: // y
				w.YankMode()
				return nil
			case 111: // o
				w.OpenMode()
				return nil
			case 97: // o
				w.ShowAttachInput()
				return nil
			}
			// pass some events on to the conversation panel
		case tcell.KeyCtrlQ:
			w.Quit()
		case tcell.KeyPgUp:
			convInputHandler(event, func(p tview.Primitive) {})
			return nil
		case tcell.KeyPgDn:
			convInputHandler(event, func(p tview.Primitive) {})
			return nil
		case tcell.KeyUp:
			convInputHandler(event, func(p tview.Primitive) {})
			return nil
		case tcell.KeyDown:
			convInputHandler(event, func(p tview.Primitive) {})
			return nil
		case tcell.KeyEnd:
			convInputHandler(event, func(p tview.Primitive) {})
			return nil
		case tcell.KeyHome:
			convInputHandler(event, func(p tview.Primitive) {})
			return nil
		case tcell.KeyESC:
			w.NormalMode()
			w.HideStatusBar()
			return nil
		case tcell.KeyCtrlT:
			w.ShowContactSearch()
			return nil
		case tcell.KeyCtrlN:
			w.NextUnreadMessage()
			return nil
		}
		return event
	}
	w.yankKeybinds = func(event *tcell.EventKey) *tcell.EventKey {
		log.Debugf("Key Event <YANK>: %v mods: %v rune: %v", event.Key(), event.Modifiers(), event.Rune())
		switch event.Key() {
		case tcell.KeyRune:
			switch event.Rune() {
			case 121: // y
				w.YankLastMsg()
				return nil
			case 108: // l
				w.YankLastLink()
				return nil
			}
		case tcell.KeyCtrlQ:
			w.Quit()
		case tcell.KeyESC:
			w.NormalMode()
			return nil
		}
		return event
	}
	w.openKeybinds = func(event *tcell.EventKey) *tcell.EventKey {
		log.Debugf("Key Event <OPEN>: %v mods: %v rune: %v", event.Key(), event.Modifiers(), event.Rune())
		switch event.Key() {
		case tcell.KeyRune:
			switch event.Rune() {
			case 108: // l
				w.OpenLastLink()
				return nil
			case 111: // o
				w.OpenLastAttachment()
				return nil
			}
		case tcell.KeyCtrlQ:
			w.Quit()
		case tcell.KeyESC:
			w.NormalMode()
			return nil
		}
		return event
	}
	w.SetInputCapture(w.normalKeybinds)

	// primitiv, row, col, rowSpan, colSpan, minGridHeight, maxGridHeight, focus)
	// TODO: lets make some of the spans confiurable?
	w.AddItem(w.contactsPanel, 0, 0, 2, 1, 0, 0, false)
	w.AddItem(w.conversationPanel, 0, 1, 1, 1, 0, 0, false)
	w.AddItem(w.sendPanel, 1, 1, 1, 1, 0, 0, false)

	if w.siggo.Config().HidePanelTitles {
		w.contactsPanel.SetTitle("")
		w.sendPanel.SetTitle("")
		w.conversationPanel.SetTitle("")
		w.conversationPanel.hideTitle = true
	}
	if w.siggo.Config().HidePhoneNumbers {
		w.conversationPanel.hidePhoneNumber = true
	}

	w.siggo = siggo
	contacts := siggo.Contacts().SortedByIndex()
	if len(contacts) > 0 {
		w.currentContact = contacts[0]
	}
	// update gui when events happen in siggo
	w.update()
	w.conversationPanel.ScrollToEnd()
	siggo.NewInfo = func(conv *model.Conversation) {
		app.QueueUpdateDraw(func() {
			w.update()
		})
	}
	siggo.ErrorEvent = w.SetErrorStatus
	return w
}

// FancyCompose opens up EDITOR and composes a big fancy message.
func FancyCompose() (string, error) {
	tmpFile, err := ioutil.TempFile(os.TempDir(), "siggo-compose-")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file for compose: %v", err)
	}
	editor := os.Getenv("EDITOR")
	if editor == "" {
		return "", fmt.Errorf("cannot compose: no $EDITOR set in environment")
	}
	fname := tmpFile.Name()
	defer os.Remove(fname)
	cmd := exec.Command(editor, fname)
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	if err = cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to start editor: %v", err)
	}
	b, err := ioutil.ReadFile(fname)
	if err != nil {
		return "", fmt.Errorf("failed to read temp file: %v", err)
	}
	return string(b), nil
}
