package utils

import (
	"strings"

	"github.com/tidwall/gjson"
)

// ExtractPrompt extracts prompt text from various standard and vendor-specific request bodies.
func ExtractPrompt(body []byte) string {
	if prompt := strings.TrimSpace(gjson.GetBytes(body, "prompt").String()); prompt != "" {
		return prompt
	}
	if text := strings.TrimSpace(gjson.GetBytes(body, "text").String()); text != "" {
		return text
	}
	if inputPrompt := strings.TrimSpace(gjson.GetBytes(body, "input.prompt").String()); inputPrompt != "" {
		return inputPrompt
	}

	contentArray := gjson.GetBytes(body, "content").Array()
	for _, node := range contentArray {
		if node.Get("type").String() == "text" {
			if txt := strings.TrimSpace(node.Get("text").String()); txt != "" {
				return txt
			}
		}
	}
	return ""
}

// ExtractNegativePrompt extracts negative prompt text from various request fields.
func ExtractNegativePrompt(body []byte) string {
	if neg := strings.TrimSpace(gjson.GetBytes(body, "extra_body.negative_prompt").String()); neg != "" {
		return neg
	}
	if neg := strings.TrimSpace(gjson.GetBytes(body, "negative_prompt").String()); neg != "" {
		return neg
	}
	if neg := strings.TrimSpace(gjson.GetBytes(body, "input.negative_prompt").String()); neg != "" {
		return neg
	}
	return ""
}

// ExtractImages extracts image URLs or base64 data URIs from various image parameters.
func ExtractImages(body []byte) []string {
	var images []string

	// 1. Check "images" array
	if imgs := gjson.GetBytes(body, "images"); imgs.IsArray() {
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

	// 2. Check "image" field
	if img := gjson.GetBytes(body, "image"); img.Exists() {
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

	// 3. Check "input_reference" field
	if ref := strings.TrimSpace(gjson.GetBytes(body, "input_reference").String()); ref != "" {
		images = append(images, ref)
	}

	// 4. Check "reference_images" field
	if refs := gjson.GetBytes(body, "reference_images"); refs.IsArray() {
		for _, node := range refs.Array() {
			if u := strings.TrimSpace(node.String()); u != "" {
				images = append(images, u)
			}
		}
	}

	// 5. Check "content" array
	if contentArray := gjson.GetBytes(body, "content").Array(); len(contentArray) > 0 {
		for _, node := range contentArray {
			itemType := node.Get("type").String()
			if itemType == "image_url" || itemType == "image" {
				if u := strings.TrimSpace(node.Get("image_url.url").String()); u != "" {
					images = append(images, u)
				} else if u := strings.TrimSpace(node.Get("image_url").String()); u != "" {
					images = append(images, u)
				} else if u := strings.TrimSpace(node.Get("url").String()); u != "" {
					images = append(images, u)
				} else if u := strings.TrimSpace(node.Get("image").String()); u != "" {
					images = append(images, u)
				}
			}
		}
	}

	return images
}

// HasVideoInput returns true if the input payload contains video references.
func HasVideoInput(body []byte) bool {
	if v := strings.TrimSpace(gjson.GetBytes(body, "video").String()); v != "" {
		return true
	}
	if v := strings.TrimSpace(gjson.GetBytes(body, "video_url").String()); v != "" {
		return true
	}
	if v := strings.TrimSpace(gjson.GetBytes(body, "input_video").String()); v != "" {
		return true
	}
	contentArray := gjson.GetBytes(body, "content").Array()
	for _, node := range contentArray {
		if node.Get("type").String() == "video_url" || node.Get("type").String() == "video" {
			return true
		}
	}
	return false
}
