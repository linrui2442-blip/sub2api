package service

// Gateway audit protocol identifiers are shared by request handlers and the
// provider-neutral prompt-audit coordinator.
const (
	SecurityAuditProtocolAnthropicMessages = "anthropic_messages"
	SecurityAuditProtocolOpenAIChat        = "openai_chat_completions"
	SecurityAuditProtocolOpenAIResponses   = "openai_responses"
	SecurityAuditProtocolOpenAIImages      = "openai_images"
	SecurityAuditProtocolGemini            = "gemini"
)
