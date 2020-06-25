package signal

import (
	"path/filepath"
)

type Message struct {
	Envelope *Envelope `json:"envelope"`
}

type Envelope struct {
	Source         string          `json:"source"`
	Timestamp      int64           `json:"timestamp"`
	IsReceipt      bool            `json:"isReceipt"`
	SyncMessage    *SyncMessage    `json:"syncMessage"`
	CallMessage    *CallMessage    `json:"callMessage"`
	ReceiptMessage *ReceiptMessage `json:"receiptMessage"`
	DataMessage    *DataMessage    `json:"dataMessage"`
	SourceDevice   int             `json:"sourceDevice"`
}

type SyncMessage struct {
	SentMessage  *SentMessage `json:"sentMessage"`
	Type         interface{}  `json:"type"`
	ReadMessages interface{}  `json:"readMessages"`
}

type SentMessage struct {
	Timestamp        int64         `json:"timestamp"`
	Message          string        `json:"message"`
	ExpiresInSeconds int64         `json:"expiresInSeconds"`
	Attachments      []*Attachment `json:"attachments"`
	GroupInfo        interface{}   `json:"groupInfo"`
	Destination      string        `json:"destination"`
}

type DataMessage struct {
	Timestamp        int64         `json:"timestamp"`
	Message          string        `json:"message"`
	ExpiresInSeconds int64         `json:"expiresInSeconds"`
	Attachments      []*Attachment `json:"attachments"`
	GroupInfo        interface{}   `json:"groupInfo"`
}

type CallMessage interface{}

type ReceiptMessage struct {
	When       int64   `json:"when"`
	IsDelivery bool    `json:"isDelivery"`
	IsRead     bool    `json:"isRead"`
	Timestamps []int64 `json:"timestamps"`
}

type Attachment struct {
	ContentType string `json:"contentType"`
	Filename    string `json:"filename"`
	ID          string `json:"id"`
	Size        int    `json:"size"`
}

// Path returns the full path to an attachment file
func (a *Attachment) Path() (string, error) {
	if a.ID == "" {
		// TODO: save our own copy of the attachment with our own ID
		// for now, just return the path where we attached it
		return a.Filename, nil
	}
	folder, err := GetSignalFolder()
	if err != nil {
		return "", err
	}
	return filepath.Join(folder, "attachments", a.ID), nil
}
