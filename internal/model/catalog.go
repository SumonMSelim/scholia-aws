package model

// Providers a chat model can come from. A user stores one key per provider.
const (
	ProviderBedrock = "bedrock"
	ProviderOpenAI  = "openai"
)

// OpenAIBaseURL is the chat completions root for a user's OpenAI key.
const OpenAIBaseURL = "https://api.openai.com/v1"

// Model kinds. Chat pickers offer only chat models; the embedding model is listed
// so the client can show which one indexes course material.
const (
	KindChat      = "chat"
	KindEmbedding = "embedding"
)

// EmbedModel is Titan Text Embeddings V2, the default embedding model. It
// matches embed.DefaultModel, which this package cannot import.
const EmbedModel = "amazon.titan-embed-text-v2:0"

// Entry is one model the web client lists.
type Entry struct {
	ID       string
	Provider string
	Label    string
	Kind     string
}

// Catalog lists the chat models Scholia can call. Bedrock ids are US inference
// profiles, the form Converse needs in a US region. OpenAI ids go to chat completions.
var Catalog = []Entry{
	{ID: DefaultModel, Provider: ProviderBedrock, Label: "Amazon Nova 2 Lite", Kind: KindChat},
	{ID: "us.amazon.nova-pro-v1:0", Provider: ProviderBedrock, Label: "Amazon Nova Pro", Kind: KindChat},
	{ID: "gpt-4.1", Provider: ProviderOpenAI, Label: "GPT-4.1", Kind: KindChat},
	{ID: "gpt-4.1-mini", Provider: ProviderOpenAI, Label: "GPT-4.1 mini", Kind: KindChat},
	{ID: "gpt-4o-mini", Provider: ProviderOpenAI, Label: "GPT-4o mini", Kind: KindChat},
	{ID: EmbedModel, Provider: ProviderBedrock, Label: "Titan Text Embeddings V2", Kind: KindEmbedding},
}

// ProviderOf reports which provider serves a catalog chat model. An embedding
// model is not a chat model, so it is not found here.
func ProviderOf(id string) (string, bool) {
	for _, entry := range Catalog {
		if entry.ID == id && entry.Kind == KindChat {
			return entry.Provider, true
		}
	}
	return "", false
}
