package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/smtp"
	"net/textproto"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/minio/minio-go/v7"
)

const maxAppFileSize = 10 << 20

func splitCSV(s string) []string {
	out := []string{}
	for _, value := range strings.Split(s, ",") {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func (a *App) dispatchNotifications(body []byte) {
	urls := splitCSV(env("WEBHOOK_URLS", ""))
	teamURL := strings.TrimSpace(env("TEAMS_WEBHOOK_URL", ""))
	if len(urls) > 0 || teamURL != "" {
		client := &http.Client{Timeout: 5 * time.Second}
		for _, endpoint := range urls {
			go func(endpoint string) {
				u, parseErr := url.Parse(endpoint)
				if parseErr != nil || u == nil || (u.Scheme != "https" && !(u.Scheme == "http" && strings.EqualFold(env("APP_ENV", "production"), "development"))) {
					log.Printf("webhook endpoint rejected: HTTPS is required outside development")
					return
				}
				req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(string(body)))
				if err != nil {
					log.Printf("webhook request: %v", err)
					return
				}
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("User-Agent", "fast-crud-webhook/1.0")
				if secret := env("WEBHOOK_HMAC_SECRET", ""); secret != "" {
					mac := hmac.New(sha256.New, []byte(secret))
					mac.Write(body)
					req.Header.Set("X-FastCRUD-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
				}
				resp, err := client.Do(req)
				if err != nil {
					log.Printf("webhook delivery failed: %v", err)
					return
				}
				resp.Body.Close()
				if resp.StatusCode < 200 || resp.StatusCode >= 300 {
					log.Printf("webhook endpoint returned HTTP %d", resp.StatusCode)
				}
			}(endpoint)
		}
		if teamURL != "" {
			var event Event
			if json.Unmarshal(body, &event) == nil {
				card := map[string]any{
					"type": "message",
					"attachments": []map[string]any{{
						"contentType": "application/vnd.microsoft.card.adaptive",
						"content": map[string]any{
							"$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
							"type":    "AdaptiveCard",
							"version": "1.5",
							"body": []map[string]any{
								{
									"type": "TextBlock",
									"text": fmt.Sprintf("%s: %s", env("APP_NAME", "Fast CRUD"), event.Type),
									"wrap": true,
								},
								{
									"type": "TextBlock",
									"text": fmt.Sprintf("Item ID: %d\nName: %s\nTime: %s", event.Item.ID, event.Item.Name, event.Timestamp.Format(time.RFC3339)),
									"wrap": true,
								},
							},
						},
					}},
				}
				if payload, err := json.Marshal(card); err == nil {
					go func() {
						req, err := http.NewRequest(http.MethodPost, teamURL, strings.NewReader(string(payload)))
						if err != nil {
							log.Printf("Teams webhook request: %v", err)
							return
						}
						req.Header.Set("Content-Type", "application/json")
						resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
						if err != nil {
							log.Printf("Teams webhook failed: %v", err)
							return
						}
						resp.Body.Close()
						if resp.StatusCode < 200 || resp.StatusCode >= 300 {
							log.Printf("Teams webhook returned HTTP %d", resp.StatusCode)
						}
					}()
				}
			}
		}
	}
	if env("SMTP_HOST", "") != "" && len(splitCSV(env("EMAIL_NOTIFY_TO", ""))) > 0 {
		var event Event
		if json.Unmarshal(body, &event) == nil {
			subject := fmt.Sprintf("%s: item %s", env("APP_NAME", "Fast CRUD"), event.Type)
			text := fmt.Sprintf("Event: %s\nItem ID: %d\nName: %s\nTime: %s\n", event.Type, event.Item.ID, event.Item.Name, event.Timestamp.Format(time.RFC3339))
			if err := sendEmail(subject, text); err != nil {
				log.Printf("event email failed: %v", err)
			}
		}
	}
}

func sendEmail(subject, body string) error {
	host := env("SMTP_HOST", "")
	port := env("SMTP_PORT", "587")
	from := env("EMAIL_FROM", "")
	to := splitCSV(env("EMAIL_NOTIFY_TO", ""))
	if host == "" || from == "" || len(to) == 0 {
		return fmt.Errorf("SMTP_HOST, EMAIL_FROM, and EMAIL_NOTIFY_TO must be configured")
	}
	message := []byte("From: " + from + "\r\nTo: " + strings.Join(to, ", ") + "\r\nSubject: " + strings.ReplaceAll(subject, "\r", "") + "\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + body)
	var auth smtp.Auth
	if user := env("SMTP_USERNAME", ""); user != "" {
		auth = smtp.PlainAuth("", user, env("SMTP_PASSWORD", ""), host)
	}
	return smtp.SendMail(host+":"+port, auth, from, to, message)
}

func (a *App) integrationStatus(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"s3": fiber.Map{"enabled": a.s3 != nil, "bucket": func() string {
			if a.s3 != nil {
				return a.cfg.S3Bucket
			}
			return ""
		}()},
		"webhooks": fiber.Map{"enabled": len(splitCSV(env("WEBHOOK_URLS", ""))) > 0},
		"teams":    fiber.Map{"enabled": strings.TrimSpace(env("TEAMS_WEBHOOK_URL", "")) != ""},
		"email":    fiber.Map{"enabled": env("SMTP_HOST", "") != "" && env("EMAIL_FROM", "") != ""},
	})
}

func (a *App) testEmail(c *fiber.Ctx) error {
	if err := sendEmail("Fast CRUD integration test", "This is a test message from Fast CRUD."); err != nil {
		return fiber.NewError(502, "email delivery failed; check SMTP configuration and application logs")
	}
	return c.JSON(fiber.Map{"status": "sent"})
}

func (a *App) testWebhook(c *fiber.Ctx) error {
	urls := splitCSV(env("WEBHOOK_URLS", ""))
	if teamURL := strings.TrimSpace(env("TEAMS_WEBHOOK_URL", "")); teamURL != "" {
		urls = append(urls, teamURL)
	}
	if len(urls) == 0 {
		return fiber.NewError(409, "no webhook endpoints configured")
	}
	body, _ := json.Marshal(fiber.Map{"type": "integration.test", "timestamp": time.Now().UTC(), "application": env("APP_NAME", "Fast CRUD"), "message": "Webhook integration test"})
	client := &http.Client{Timeout: 5 * time.Second}
	results := []fiber.Map{}
	for _, endpoint := range urls {
		u, err := url.Parse(endpoint)
		if err != nil || u == nil || (u.Scheme != "https" && !(u.Scheme == "http" && strings.EqualFold(env("APP_ENV", "production"), "development"))) {
			host := "invalid-url"
			if u != nil {
				host = u.Host
			}
			results = append(results, fiber.Map{"endpoint": host, "status": "rejected_non_https"})
			continue
		}
		req, _ := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		if secret := env("WEBHOOK_HMAC_SECRET", ""); secret != "" {
			mac := hmac.New(sha256.New, []byte(secret))
			mac.Write(body)
			req.Header.Set("X-FastCRUD-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
		}
		resp, err := client.Do(req)
		if err != nil {
			results = append(results, fiber.Map{"endpoint": u.Host, "status": "failed"})
			continue
		}
		resp.Body.Close()
		results = append(results, fiber.Map{"endpoint": u.Host, "http_status": resp.StatusCode})
	}
	return c.JSON(fiber.Map{"results": results})
}

func (a *App) uploadFile(c *fiber.Ctx) error {
	if a.s3 == nil {
		return fiber.NewError(404, "application-level S3 storage is disabled")
	}
	file, err := c.FormFile("file")
	if err != nil {
		return fiber.NewError(400, "multipart field 'file' is required")
	}
	if file.Size < 1 || file.Size > maxAppFileSize {
		return fiber.NewError(413, "file size must be between 1 byte and 10 MiB")
	}
	f, err := file.Open()
	if err != nil {
		return fiber.ErrBadRequest
	}
	defer f.Close()
	keyToken, err := randomToken(18)
	if err != nil {
		return fiber.ErrInternalServerError
	}
	name := filepath.Base(file.Filename)
	name = strings.ReplaceAll(name, "\\", "_")
	key := time.Now().UTC().Format("2006/01/02") + "/" + keyToken + "/" + name
	ctx, cancel := context.WithTimeout(c.UserContext(), 30*time.Second)
	defer cancel()
	info, err := a.s3.PutObject(ctx, a.cfg.S3Bucket, key, f, file.Size, minio.PutObjectOptions{ContentType: safeContentType(file.Header)})
	if err != nil {
		log.Printf("S3 upload failed: %v", err)
		return fiber.NewError(502, "object storage upload failed")
	}
	a.audit(ctx, c, "upload", "object", key, "application file uploaded")
	return c.Status(201).JSON(fiber.Map{"key": key, "size": info.Size, "bucket": a.cfg.S3Bucket})
}

func safeContentType(h textproto.MIMEHeader) string {
	ct := h.Get("Content-Type")
	if ct == "" || strings.ContainsAny(ct, "\r\n") {
		return "application/octet-stream"
	}
	return ct
}

func (a *App) listFiles(c *fiber.Ctx) error {
	if a.s3 == nil {
		return fiber.NewError(404, "application-level S3 storage is disabled")
	}
	prefix := c.Query("prefix", "")
	if strings.Contains(prefix, "..") || strings.HasPrefix(prefix, "/") {
		return fiber.NewError(400, "invalid prefix")
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 10*time.Second)
	defer cancel()
	objects := []fiber.Map{}
	for obj := range a.s3.ListObjects(ctx, a.cfg.S3Bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if obj.Err != nil {
			return fiber.NewError(502, "object storage listing failed")
		}
		if len(objects) >= 100 {
			break
		}
		objects = append(objects, fiber.Map{"key": obj.Key, "size": obj.Size, "last_modified": obj.LastModified})
	}
	return c.JSON(fiber.Map{"data": objects, "limit": 100})
}

func (a *App) downloadFile(c *fiber.Ctx) error {
	if a.s3 == nil {
		return fiber.NewError(404, "application-level S3 storage is disabled")
	}
	key := c.Query("key", "")
	if key == "" || strings.Contains(key, "..") || strings.HasPrefix(key, "/") {
		return fiber.NewError(400, "valid key query parameter is required")
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 5*time.Second)
	defer cancel()
	link, err := a.s3.PresignedGetObject(ctx, a.cfg.S3Bucket, key, 5*time.Minute, url.Values{})
	if err != nil {
		return fiber.NewError(502, "could not create download link")
	}
	return c.JSON(fiber.Map{"url": link.String(), "expires_in_seconds": 300})
}

func (a *App) deleteFile(c *fiber.Ctx) error {
	if a.s3 == nil {
		return fiber.NewError(404, "application-level S3 storage is disabled")
	}
	key := c.Query("key", "")
	if key == "" || strings.Contains(key, "..") || strings.HasPrefix(key, "/") {
		return fiber.NewError(400, "valid key query parameter is required")
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 10*time.Second)
	defer cancel()
	if err := a.s3.RemoveObject(ctx, a.cfg.S3Bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fiber.NewError(502, "object storage delete failed")
	}
	a.audit(ctx, c, "delete", "object", key, "application file deleted")
	return c.SendStatus(204)
}
