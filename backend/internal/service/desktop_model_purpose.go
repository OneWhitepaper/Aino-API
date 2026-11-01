package service

const (
	DesktopPurposeChat  = "chat"
	DesktopPurposeImage = "image"
)

func validateDesktopImageQualities(entry DesktopModelEntry) error {
	options := entry.Capabilities.ImageQualityOptions
	if len(options) > 0 && entry.APIMode != "images_generations" {
		return invalidDesktopSettings("image quality options require the Images API protocol")
	}
	allowed := map[string]bool{"auto": true, "low": true, "medium": true, "high": true, "xhigh": true, "max": true}
	seen := map[string]bool{}
	for _, quality := range options {
		if !allowed[quality] || seen[quality] {
			return invalidDesktopSettings("image quality options must be supported unique values")
		}
		seen[quality] = true
	}
	return nil
}

func desktopModelPurpose(entry DesktopModelEntry) string {
	if entry.APIMode == "images_generations" {
		return DesktopPurposeImage
	}
	return DesktopPurposeChat
}

func desktopModelSupportsPurpose(entry DesktopModelEntry, purpose string) bool {
	if desktopModelPurpose(entry) != purpose {
		return false
	}
	if purpose == DesktopPurposeImage {
		return entry.Capabilities.ImageGeneration && (entry.Platform == PlatformOpenAI || entry.Platform == PlatformGrok)
	}
	return entry.AgentVerified && entry.Capabilities.Tools
}
