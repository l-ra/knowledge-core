package datatype

import (
	"fmt"
	"net/url"
	"strings"
)

// NormalizeIRIBase validates an absolute http(s) IRI base ending with "/" or "#".
// Empty string is allowed (means fallback to platform default namespace).
func NormalizeIRIBase(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", nil
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", fmt.Errorf("invalid iriBase: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("iriBase must be http(s)")
	}
	if u.Host == "" {
		return "", fmt.Errorf("iriBase must include host")
	}
	if !strings.HasSuffix(s, "/") && !strings.HasSuffix(s, "#") {
		s += "/"
	}
	return s, nil
}

// NormalizeIRILocal validates a relative path segment for use after iriBase.
// Empty means "use public_id at projection time".
func NormalizeIRILocal(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", nil
	}
	if strings.ContainsAny(s, " \t\n\r") {
		return "", fmt.Errorf("iriLocal must not contain whitespace")
	}
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return "", fmt.Errorf("iriLocal must be relative, not absolute")
	}
	s = strings.TrimPrefix(s, "/")
	if s == "" {
		return "", fmt.Errorf("iriLocal empty after normalize")
	}
	return s, nil
}

// ResolveIRI builds canonical export IRI from package base + local (or publicID fallback).
func ResolveIRI(iriBase, iriLocal, publicID, fallbackNS string) string {
	local := iriLocal
	if local == "" {
		local = publicID
	}
	if iriBase != "" {
		return iriBase + local
	}
	if fallbackNS == "" {
		fallbackNS = "https://knowledge-core.local/entity/"
	}
	return fallbackNS + publicID
}
