package service

import (
	"context"
	"net/http"
	"testing"
)

func TestHydrateOpenAIResponsesImagePointersUsesSSEInlineAsset(t *testing.T) {
	const imageBase64 = "iVBORw0KGgo="
	body := []byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"response-1\",\"output\":[{\"type\":\"image_generation_call\",\"result\":\"file-service://asset-1\",\"asset_pointer\":\"file-service://asset-1\",\"b64_json\":\"" + imageBase64 + "\"}]}}\n\n")

	results, _, _, _, foundFinal, err := collectOpenAIImagesFromResponsesBody(body)
	if err != nil {
		t.Fatalf("collect response images: %v", err)
	}
	if !foundFinal || len(results) != 1 {
		t.Fatalf("expected one completed image result, foundFinal=%v results=%d", foundFinal, len(results))
	}
	pointers := collectOpenAIImagePointers(body)
	forEachOpenAISSEDataPayload(string(body), func(payload []byte) {
		pointers = mergeOpenAIImagePointerInfos(pointers, collectOpenAIImagePointers(payload))
	})
	if len(pointers) != 1 || pointers[0].B64JSON != imageBase64 {
		t.Fatalf("inline asset was not collected: %#v", pointers)
	}

	svc := &OpenAIGatewayService{}
	if err := svc.hydrateOpenAIResponsesImagePointers(context.Background(), nil, http.Header{}, body, results); err != nil {
		t.Fatalf("hydrate embedded image: %v", err)
	}
	if results[0].Result != imageBase64 {
		t.Fatalf("result = %q, want embedded base64", results[0].Result)
	}
	if results[0].OutputFormat != "png" {
		t.Fatalf("output format = %q, want png", results[0].OutputFormat)
	}
}
