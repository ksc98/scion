// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package hubclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path/filepath"
	"time"

	"github.com/GoogleCloudPlatform/scion/pkg/apiclient"
	"github.com/GoogleCloudPlatform/scion/pkg/transfer"
)

// TemplateService handles template operations.
type TemplateService interface {
	// List returns templates matching the filter criteria.
	List(ctx context.Context, opts *ListTemplatesOptions) (*ListTemplatesResponse, error)

	// Get returns a single template by ID.
	Get(ctx context.Context, templateID string) (*Template, error)

	// Create creates a new template.
	Create(ctx context.Context, req *CreateTemplateRequest) (*CreateTemplateResponse, error)

	// Update updates a template.
	Update(ctx context.Context, templateID string, req *UpdateTemplateRequest) (*Template, error)

	// Delete removes a template.
	Delete(ctx context.Context, templateID string) error

	// Clone creates a copy of a template.
	Clone(ctx context.Context, templateID string, req *CloneTemplateRequest) (*Template, error)

	// RequestUploadURLs requests signed URLs for uploading template files.
	RequestUploadURLs(ctx context.Context, templateID string, files []FileUploadRequest) (*UploadResponse, error)

	// Finalize finalizes a template after file upload.
	Finalize(ctx context.Context, templateID string, manifest *TemplateManifest) (*Template, error)

	// RequestDownloadURLs requests signed URLs for downloading template files.
	RequestDownloadURLs(ctx context.Context, templateID string) (*DownloadResponse, error)

	// UploadFile uploads a file to the given signed URL.
	UploadFile(ctx context.Context, url string, method string, headers map[string]string, content io.Reader) error

	// DownloadFile downloads a file from the given signed URL.
	DownloadFile(ctx context.Context, url string) ([]byte, error)

	// ListFiles returns the file manifest for a template via direct hub endpoint.
	ListFiles(ctx context.Context, templateID string) (*TemplateFileListResponse, error)

	// ReadFile returns the content of a single template file via direct hub endpoint.
	ReadFile(ctx context.Context, templateID, filePath string) (*TemplateFileContentResponse, error)

	// WriteFile writes content to a single template file via direct hub endpoint.
	// The hub stores the file and updates the manifest + content hash atomically.
	WriteFile(ctx context.Context, templateID, filePath, content string) (*TemplateFileWriteResponse, error)

	// UploadFiles uploads multiple files via multipart POST to the hub.
	// The hub stores all files and updates the manifest + content hash atomically.
	UploadFiles(ctx context.Context, templateID string, files map[string][]byte) (*TemplateFileUploadResponse, error)
}

// templateService is the implementation of TemplateService.
type templateService struct {
	c              *client
	transferClient *transfer.Client
}

// ListTemplatesOptions configures template list filtering.
type ListTemplatesOptions struct {
	Name    string // Filter by exact template name
	Search  string // Full-text search on name/description
	Scope   string // Filter by scope (global, grove, user)
	GroveID string // Filter by grove
	Harness string // Filter by harness type
	Status  string // Filter by status (active, archived)
	Page    apiclient.PageOptions
}

// ListTemplatesResponse is the response from listing templates.
type ListTemplatesResponse struct {
	Templates []Template
	Page      apiclient.PageResult
}

// CreateTemplateRequest is the request for creating a template.
type CreateTemplateRequest struct {
	Name       string          `json:"name"`
	Harness    string          `json:"harness,omitempty"`
	Scope      string          `json:"scope"`
	GroveID    string          `json:"groveId,omitempty"`
	Config     *TemplateConfig `json:"config,omitempty"`
	Visibility string          `json:"visibility,omitempty"`
}

// UpdateTemplateRequest is the request for updating a template.
type UpdateTemplateRequest struct {
	Name       string          `json:"name,omitempty"`
	Config     *TemplateConfig `json:"config,omitempty"`
	Visibility string          `json:"visibility,omitempty"`
}

// CloneTemplateRequest is the request for cloning a template.
type CloneTemplateRequest struct {
	Name    string `json:"name"`
	Scope   string `json:"scope"`
	GroveID string `json:"groveId,omitempty"`
}

// FileUploadRequest describes a file to upload.
type FileUploadRequest struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// CreateTemplateResponse is the response from creating a template.
type CreateTemplateResponse struct {
	Template    *Template       `json:"template"`
	UploadURLs  []UploadURLInfo `json:"uploadUrls,omitempty"`
	ManifestURL string          `json:"manifestUrl,omitempty"`
}

// UploadURLInfo contains a signed URL for uploading a file.
type UploadURLInfo struct {
	Path    string            `json:"path"`
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers,omitempty"`
	Expires time.Time         `json:"expires"`
}

// UploadResponse is the response containing signed upload URLs.
type UploadResponse struct {
	UploadURLs  []UploadURLInfo `json:"uploadUrls"`
	ManifestURL string          `json:"manifestUrl,omitempty"`
}

// FinalizeRequest is the request body for finalizing a template upload.
type FinalizeRequest struct {
	Manifest *TemplateManifest `json:"manifest"`
}

// TemplateManifest is the manifest of uploaded template files.
type TemplateManifest struct {
	Version string         `json:"version"`
	Harness string         `json:"harness,omitempty"`
	Files   []TemplateFile `json:"files"`
}

// DownloadResponse contains signed URLs for downloading template files.
type DownloadResponse struct {
	ManifestURL string            `json:"manifestUrl,omitempty"`
	Files       []DownloadURLInfo `json:"files"`
	Expires     time.Time         `json:"expires"`
}

// DownloadURLInfo contains info for downloading a file.
type DownloadURLInfo struct {
	Path string `json:"path"`
	URL  string `json:"url"`
	Size int64  `json:"size"`
	Hash string `json:"hash,omitempty"`
}

// TemplateFileListResponse is the response for listing template files directly.
type TemplateFileListResponse struct {
	Files      []TemplateFileEntry `json:"files"`
	TotalSize  int64               `json:"totalSize"`
	TotalCount int                 `json:"totalCount"`
}

// TemplateFileEntry is a single file in the template file listing.
type TemplateFileEntry struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	ModTime string `json:"modTime"`
	Mode    string `json:"mode"`
}

// TemplateFileContentResponse is the response for reading a template file directly.
type TemplateFileContentResponse struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Size     int64  `json:"size"`
	ModTime  string `json:"modTime"`
	Encoding string `json:"encoding"`
	Hash     string `json:"hash,omitempty"`
}

// TemplateFileWriteResponse is the response after writing a template file directly.
type TemplateFileWriteResponse struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	Hash    string `json:"hash"`
	ModTime string `json:"modTime"`
}

// TemplateFileUploadResponse is the response after uploading template files directly.
type TemplateFileUploadResponse struct {
	Files []TemplateFileEntry `json:"files"`
	Hash  string              `json:"hash"`
}

// List returns templates matching the filter criteria.
func (s *templateService) List(ctx context.Context, opts *ListTemplatesOptions) (*ListTemplatesResponse, error) {
	query := url.Values{}
	if opts != nil {
		if opts.Name != "" {
			query.Set("name", opts.Name)
		}
		if opts.Search != "" {
			query.Set("search", opts.Search)
		}
		if opts.Scope != "" {
			query.Set("scope", opts.Scope)
		}
		if opts.GroveID != "" {
			query.Set("groveId", opts.GroveID)
		}
		if opts.Harness != "" {
			query.Set("harness", opts.Harness)
		}
		if opts.Status != "" {
			query.Set("status", opts.Status)
		}
		opts.Page.ToQuery(query)
	}

	resp, err := s.c.transport.GetWithQuery(ctx, "/api/v1/templates", query, nil)
	if err != nil {
		return nil, err
	}

	type listResponse struct {
		Templates  []Template `json:"templates"`
		NextCursor string     `json:"nextCursor,omitempty"`
		TotalCount int        `json:"totalCount,omitempty"`
	}

	result, err := apiclient.DecodeResponse[listResponse](resp)
	if err != nil {
		return nil, err
	}

	return &ListTemplatesResponse{
		Templates: result.Templates,
		Page: apiclient.PageResult{
			NextCursor: result.NextCursor,
			TotalCount: result.TotalCount,
		},
	}, nil
}

// Get returns a single template by ID.
func (s *templateService) Get(ctx context.Context, templateID string) (*Template, error) {
	resp, err := s.c.transport.Get(ctx, "/api/v1/templates/"+templateID, nil)
	if err != nil {
		return nil, err
	}
	return apiclient.DecodeResponse[Template](resp)
}

// Create creates a new template.
func (s *templateService) Create(ctx context.Context, req *CreateTemplateRequest) (*CreateTemplateResponse, error) {
	resp, err := s.c.transport.Post(ctx, "/api/v1/templates", req, nil)
	if err != nil {
		return nil, err
	}
	return apiclient.DecodeResponse[CreateTemplateResponse](resp)
}

// Update updates a template.
func (s *templateService) Update(ctx context.Context, templateID string, req *UpdateTemplateRequest) (*Template, error) {
	resp, err := s.c.transport.Patch(ctx, "/api/v1/templates/"+templateID, req, nil)
	if err != nil {
		return nil, err
	}
	return apiclient.DecodeResponse[Template](resp)
}

// Delete removes a template.
func (s *templateService) Delete(ctx context.Context, templateID string) error {
	resp, err := s.c.transport.Delete(ctx, "/api/v1/templates/"+templateID, nil)
	if err != nil {
		return err
	}
	return apiclient.CheckResponse(resp)
}

// Clone creates a copy of a template.
func (s *templateService) Clone(ctx context.Context, templateID string, req *CloneTemplateRequest) (*Template, error) {
	resp, err := s.c.transport.Post(ctx, "/api/v1/templates/"+templateID+"/clone", req, nil)
	if err != nil {
		return nil, err
	}
	return apiclient.DecodeResponse[Template](resp)
}

// RequestUploadURLs requests signed URLs for uploading template files.
func (s *templateService) RequestUploadURLs(ctx context.Context, templateID string, files []FileUploadRequest) (*UploadResponse, error) {
	req := struct {
		Files []FileUploadRequest `json:"files"`
	}{
		Files: files,
	}
	resp, err := s.c.transport.Post(ctx, "/api/v1/templates/"+templateID+"/upload", req, nil)
	if err != nil {
		return nil, err
	}
	return apiclient.DecodeResponse[UploadResponse](resp)
}

// Finalize finalizes a template after file upload.
func (s *templateService) Finalize(ctx context.Context, templateID string, manifest *TemplateManifest) (*Template, error) {
	req := FinalizeRequest{
		Manifest: manifest,
	}
	resp, err := s.c.transport.Post(ctx, "/api/v1/templates/"+templateID+"/finalize", req, nil)
	if err != nil {
		return nil, err
	}
	return apiclient.DecodeResponse[Template](resp)
}

// RequestDownloadURLs requests signed URLs for downloading template files.
func (s *templateService) RequestDownloadURLs(ctx context.Context, templateID string) (*DownloadResponse, error) {
	resp, err := s.c.transport.Get(ctx, "/api/v1/templates/"+templateID+"/download", nil)
	if err != nil {
		return nil, err
	}
	return apiclient.DecodeResponse[DownloadResponse](resp)
}

// UploadFile uploads a file to the given signed URL.
// For local storage (file:// URLs), this writes directly to the filesystem.
// Delegates to transfer.Client.
func (s *templateService) UploadFile(ctx context.Context, signedURL string, method string, headers map[string]string, content io.Reader) error {
	client := s.getTransferClient()
	return client.UploadFileWithMethod(ctx, signedURL, method, headers, content)
}

// DownloadFile downloads a file from the given signed URL.
// For local storage (file:// URLs), this reads directly from the filesystem.
// Delegates to transfer.Client.
func (s *templateService) DownloadFile(ctx context.Context, signedURL string) ([]byte, error) {
	client := s.getTransferClient()
	return client.DownloadFile(ctx, signedURL)
}

// getTransferClient returns the transfer client, creating one if necessary.
func (s *templateService) getTransferClient() *transfer.Client {
	if s.transferClient == nil {
		s.transferClient = transfer.NewClient(s.c.transport.HTTPClient)
	}
	return s.transferClient
}

// ListFiles returns the file manifest for a template via direct hub endpoint.
func (s *templateService) ListFiles(ctx context.Context, templateID string) (*TemplateFileListResponse, error) {
	resp, err := s.c.transport.Get(ctx, "/api/v1/templates/"+templateID+"/files", nil)
	if err != nil {
		return nil, err
	}
	return apiclient.DecodeResponse[TemplateFileListResponse](resp)
}

// ReadFile returns the content of a single template file via direct hub endpoint.
func (s *templateService) ReadFile(ctx context.Context, templateID, filePath string) (*TemplateFileContentResponse, error) {
	resp, err := s.c.transport.Get(ctx, "/api/v1/templates/"+templateID+"/files/"+filePath, nil)
	if err != nil {
		return nil, err
	}
	return apiclient.DecodeResponse[TemplateFileContentResponse](resp)
}

// WriteFile writes content to a single template file via direct hub endpoint.
func (s *templateService) WriteFile(ctx context.Context, templateID, filePath, content string) (*TemplateFileWriteResponse, error) {
	req := struct {
		Content string `json:"content"`
	}{
		Content: content,
	}
	resp, err := s.c.transport.Put(ctx, "/api/v1/templates/"+templateID+"/files/"+filePath, req, nil)
	if err != nil {
		return nil, err
	}
	return apiclient.DecodeResponse[TemplateFileWriteResponse](resp)
}

// UploadFiles uploads multiple files via multipart POST to the hub.
func (s *templateService) UploadFiles(ctx context.Context, templateID string, files map[string][]byte) (*TemplateFileUploadResponse, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	for path, content := range files {
		part, err := writer.CreateFormFile(path, filepath.Base(path))
		if err != nil {
			return nil, fmt.Errorf("failed to create form file for %s: %w", path, err)
		}
		if _, err := part.Write(content); err != nil {
			return nil, fmt.Errorf("failed to write content for %s: %w", path, err)
		}
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.c.transport.BaseURL+"/api/v1/templates/"+templateID+"/files", body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := s.c.transport.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	return apiclient.DecodeResponse[TemplateFileUploadResponse](resp)
}
