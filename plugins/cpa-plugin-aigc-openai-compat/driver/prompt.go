package driver

import (
	"strings"

	"github.com/MoeForge/moego/ojson"
)

// ExtractPrompt extracts positive prompt text from request body.
func ExtractPrompt(body []byte) string {
	if prompt := strings.TrimSpace(ojson.GetBytes(body, "prompt").String()); prompt != "" {
		return prompt
	}
	if text := strings.TrimSpace(ojson.GetBytes(body, "text").String()); text != "" {
		return text
	}
	if inputPrompt := strings.TrimSpace(ojson.GetBytes(body, "input.prompt").String()); inputPrompt != "" {
		return inputPrompt
	}

	contentArray := ojson.GetBytes(body, "content").Array()
	for _, node := range contentArray {
		if node.Get("type").String() == "text" {
			if txt := strings.TrimSpace(node.Get("text").String()); txt != "" {
				return txt
			}
		}
	}
	return ""
}

// ExtractNegativePrompt extracts negative prompt from request body.
func ExtractNegativePrompt(body []byte) string {
	if neg := strings.TrimSpace(ojson.GetBytes(body, "extra_body.negative_prompt").String()); neg != "" {
		return neg
	}
	if neg := strings.TrimSpace(ojson.GetBytes(body, "negative_prompt").String()); neg != "" {
		return neg
	}
	if neg := strings.TrimSpace(ojson.GetBytes(body, "input.negative_prompt").String()); neg != "" {
		return neg
	}
	return ""
}

// ExtractImages extracts reference image URLs or base64 data URIs from request body.
func ExtractImages(body []byte) []string {
	var images []string

	// 1. "images" array
	if imgs := ojson.GetBytes(body, "images"); imgs.IsArray() {
		for _, node := range imgs.Array() {
			if node.IsObject() {
				if u := strings.TrimSpace(node.Get("image_url.url").String()); u != "" {
					images = append(images, u)
				} else if u := strings.TrimSpace(node.Get("image_url").String()); u != "" {
					images = append(images, u)
				} else if u := strings.TrimSpace(node.Get("url").String()); u != "" {
					images = append(images, u)
				} else if u := strings.TrimSpace(node.Get("image").String()); u != "" {
					images = append(images, u)
				}
			} else if u := strings.TrimSpace(node.String()); u != "" {
				images = append(images, u)
			}
		}
	}

	// 2. "image" single field
	if img := ojson.GetBytes(body, "image"); img.Exists() {
		if img.IsObject() {
			if u := strings.TrimSpace(img.Get("image_url.url").String()); u != "" {
				images = append(images, u)
			} else if u := strings.TrimSpace(img.Get("url").String()); u != "" {
				images = append(images, u)
			} else if u := strings.TrimSpace(img.Get("image").String()); u != "" {
				images = append(images, u)
			}
		} else if u := strings.TrimSpace(img.String()); u != "" {
			images = append(images, u)
		}
	}

	// 3. "input_reference"
	if ref := strings.TrimSpace(ojson.GetBytes(body, "input_reference").String()); ref != "" {
		images = append(images, ref)
	}

	// 4. "reference_images"
	if refs := ojson.GetBytes(body, "reference_images"); refs.IsArray() {
		for _, node := range refs.Array() {
			if u := strings.TrimSpace(node.String()); u != "" {
				images = append(images, u)
			}
		}
	}

	// 5. OpenAI style "content" array
	if contentArray := ojson.GetBytes(body, "content").Array(); len(contentArray) > 0 {
		for _, node := range contentArray {
			itemType := node.Get("type").String()
			if itemType == "image_url" || itemType == "image" {
				if u := strings.TrimSpace(node.Get("image_url.url").String()); u != "" {
					images = append(images, u)
				} else if u := strings.TrimSpace(node.Get("image_url").String()); u != "" {
					images = append(images, u)
				}
			}
		}
	}

	return images
}
