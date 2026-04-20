package providerutil

import (
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
)

func ExtractBodyFromRawMIME(rawMessage string) (string, error) {
	entity, err := mail.ReadMessage(strings.NewReader(rawMessage))
	if err != nil {
		return "", err
	}

	mediaType, parameters, err := mime.ParseMediaType(entity.Header.Get("Content-Type"))
	if err != nil {
		return readAll(entity.Body)
	}

	if !strings.HasPrefix(strings.ToLower(mediaType), "multipart/") {
		return readAll(entity.Body)
	}

	multipartReader := multipart.NewReader(entity.Body, parameters["boundary"])
	for {
		part, err := multipartReader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}

		contentType := strings.ToLower(part.Header.Get("Content-Type"))
		if strings.HasPrefix(contentType, "text/plain") || strings.HasPrefix(contentType, "text/html") {
			body, readErr := readAll(part)
			if readErr != nil {
				return "", readErr
			}
			if strings.TrimSpace(body) != "" {
				return body, nil
			}
		}
	}

	return "", nil
}

func readAll(reader io.Reader) (string, error) {
	payload, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}
