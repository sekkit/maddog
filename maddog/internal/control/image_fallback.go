package control

import (
	"context"
	"fmt"
	"strings"

	"maddog/internal/event"
	"maddog/internal/nilutil"
	"maddog/internal/provider"
)

const imageFallbackMaxTokens = 1024

type imageFallbackInput struct {
	label string
	data  string
}

func (c *Controller) withImageFallback(ctx context.Context, input string) string {
	if c == nil || nilutil.IsNil(c.imageFallback) || c.imageInputEnabled() || strings.Contains(input, "<image_fallback ") {
		return input
	}
	images := c.collectImageFallbackInputs(input)
	if len(images) == 0 {
		return input
	}
	summary, err := c.runImageFallback(ctx, input, images)
	if err != nil {
		c.warnImageFallback("image fallback failed; continuing without generated image context: " + err.Error())
		return input
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		c.warnImageFallback("image fallback returned an empty summary; continuing without generated image context")
		return input
	}
	return injectImageFallback(input, c.imageFallbackBlock(images, summary))
}

func (c *Controller) collectImageFallbackInputs(input string) []imageFallbackInput {
	refInput := strings.TrimSpace(StripReferencedContextPrefix(input))
	if refInput == "" {
		refInput = input
	}
	refs := resolveBareNames(c.detectRefs(refInput), c.workspaceRoot)
	seen := map[string]bool{}
	var images []imageFallbackInput
	for _, r := range refs {
		baseDir := c.workspaceRoot
		if r.baseDir != "" {
			baseDir = r.baseDir
		}
		data, err := visionRefImageDataURL(r, baseDir)
		if err != nil {
			continue
		}
		label := r.displayPath
		if label == "" {
			label = r.path
		}
		if label == "" {
			label = "@" + r.raw
		}
		key := baseDir + "\x00" + r.path + "\x00" + label
		if seen[key] {
			continue
		}
		seen[key] = true
		images = append(images, imageFallbackInput{label: label, data: data})
	}
	return images
}

func (c *Controller) runImageFallback(ctx context.Context, input string, images []imageFallbackInput) (string, error) {
	reqImages := make([]string, 0, len(images))
	for _, img := range images {
		reqImages = append(reqImages, img.data)
	}
	ch, err := c.imageFallback.Stream(ctx, provider.Request{
		Messages: []provider.Message{{
			Role:    provider.RoleUser,
			Content: imageFallbackPrompt(input, images),
			Images:  reqImages,
		}},
		Temperature: 0,
		MaxTokens:   imageFallbackMaxTokens,
	})
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkText:
			b.WriteString(chunk.Text)
		case provider.ChunkError:
			if chunk.Err != nil {
				return "", chunk.Err
			}
			return "", fmt.Errorf("provider stream returned an error")
		}
	}
	return b.String(), nil
}

func imageFallbackPrompt(input string, images []imageFallbackInput) string {
	userInput := strings.TrimSpace(StripReferencedContextPrefix(input))
	var b strings.Builder
	b.WriteString("Analyze the attached image(s) for a text-only coding agent. Return concise, factual visual context that helps answer the user's request. Include visible UI text, layout, colors, diagrams, errors, and any uncertainties. Do not mention that you received base64 data.\n\n")
	b.WriteString("Image refs:\n")
	for i, img := range images {
		fmt.Fprintf(&b, "- %d. %s\n", i+1, img.label)
	}
	if userInput != "" {
		b.WriteString("\nUser request:\n")
		b.WriteString(userInput)
	}
	return b.String()
}

func (c *Controller) imageFallbackBlock(images []imageFallbackInput, summary string) string {
	labels := make([]string, 0, len(images))
	for _, img := range images {
		labels = append(labels, img.label)
	}
	attr := `source="` + xmlAttr(strings.Join(labels, ", ")) + `" provider="` + xmlAttr(c.imageFallback.Name()) + `"`
	var b strings.Builder
	appendRefBlock(&b, "image_fallback", attr, "[derived image analysis for the text-only main model]\n"+summary)
	return b.String()
}

func injectImageFallback(input, block string) string {
	const preamble = "Referenced context:"
	s := strings.TrimSpace(input)
	if strings.HasPrefix(s, preamble) {
		rest := strings.TrimSpace(s[len(preamble):])
		if rest == "" {
			return preamble + "\n\n" + block
		}
		return preamble + "\n\n" + block + "\n\n" + rest
	}
	return preamble + "\n\n" + block + "\n\n" + input
}

func xmlAttr(s string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		`"`, "&quot;",
		"<", "&lt;",
		">", "&gt;",
	)
	return replacer.Replace(s)
}

func (c *Controller) warnImageFallback(text string) {
	if c == nil || nilutil.IsNil(c.sink) {
		return
	}
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: text})
}
