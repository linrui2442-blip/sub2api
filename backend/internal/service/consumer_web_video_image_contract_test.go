package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestConsumerWebImageVideoCapturedContract(t *testing.T) {
	file := &consumerWebUploadedFile{ID: "<FILE_REF>", Name: "<FILENAME>", MIME: "image/jpeg", AttachmentUUID: "<ATTACHMENT_UUID>"}
	inner := consumerWebVideoGenerateInner("<PROMPT>", "en", "<REQUEST_UUID>", 3, file)
	if len(inner) != 81 || inner[49] != 11 || inner[59] != "<REQUEST_UUID>" || inner[79] != 3 {
		t.Fatal("image video inner slots differ from captured structure")
	}
	option, err := json.Marshal(inner[55])
	if err != nil || string(option) != "[[17]]" {
		t.Fatal("image video option structure mismatch")
	}
	message, ok := inner[0].([]any)
	if !ok || len(message) != 10 || message[0] != "<PROMPT>" {
		t.Fatal("image video message shape mismatch")
	}
	attachments, ok := message[3].([]any)
	if !ok || len(attachments) != 1 {
		t.Fatal("image attachment count mismatch")
	}
	attachment, ok := attachments[0].([]any)
	if !ok || len(attachment) != 9 || attachment[1] != "<FILENAME>" {
		t.Fatal("image attachment descriptor shape mismatch")
	}
	data, ok := attachment[0].([]any)
	if !ok || len(data) != 5 || data[0] != "<FILE_REF>" || data[1] != 1 || data[2] != nil || data[3] != "image/jpeg" || data[4] != "<ATTACHMENT_UUID>" {
		t.Fatal("image attachment data mismatch")
	}
	mode, err := json.Marshal(message[9])
	if err != nil || string(mode) != `[null,null,null,null,null,null,[[null,null,null,2]]]` {
		t.Fatal("image video mode mismatch")
	}
	if strings.Contains(string(mode), "<FILE_REF>") {
		t.Fatal("mode contains file identity")
	}
}

func TestConsumerWebTextVideoStructureUnchanged(t *testing.T) {
	inner := consumerWebVideoGenerateInner("<PROMPT>", "en", "<REQUEST_UUID>", 3, nil)
	message, ok := inner[0].([]any)
	if !ok || len(message) != 7 || message[3] != nil || inner[49] != nil || inner[55] != nil || inner[59] != "<REQUEST_UUID>" {
		t.Fatal("text video structure changed")
	}
}

func TestConsumerWebImageVideoAttachmentUUIDIsRuntimeGenerated(t *testing.T) {
	first, second := consumerWebVideoAttachmentUUID(), consumerWebVideoAttachmentUUID()
	if first == second || first != strings.ToUpper(first) {
		t.Fatal("attachment UUID is not freshly generated")
	}
	if _, err := uuid.Parse(first); err != nil {
		t.Fatal("attachment UUID is invalid")
	}
}
