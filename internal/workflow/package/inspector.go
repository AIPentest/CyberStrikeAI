package workflowpackage

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"unicode"
)

const (
	MaxArchiveBytes   = 10 << 20
	MaxExtractedBytes = 20 << 20
)

// PackageError contains only a contract error code and safe, client-facing fields.
type PackageError struct {
	Code    string
	Message string
	Details map[string]any
}

func (e *PackageError) Error() string { return e.Code + ": " + e.Message }

func packageError(code, message string) error {
	return &PackageError{Code: code, Message: message}
}

// ErrorCode returns a package contract code without exposing internal errors.
func ErrorCode(err error) string {
	var target *PackageError
	if errors.As(err, &target) {
		return target.Code
	}
	return ""
}

type InspectionResult struct {
	PackageHash string
	Manifest    Manifest
	Document    Document
	ContentHash string
	GraphHash   string
	NodeCount   int
	EdgeCount   int
}

// InspectArchive verifies an archive without executing any package content.
// validateGraph is injected by the application so this format package has no
// dependency on the workflow runtime or database driver.
func InspectArchive(ctx context.Context, archive []byte, validateGraph func(context.Context, string) error) (*InspectionResult, error) {
	if len(archive) == 0 {
		return nil, packageError("WFPKG_FILE_REQUIRED", "a workflow package file must be uploaded")
	}
	if len(archive) > MaxArchiveBytes {
		return nil, packageError("WFPKG_FILE_TOO_LARGE", "the workflow package file exceeds the size limit")
	}
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, packageError("WFPKG_INVALID_ARCHIVE", "the workflow package is not a valid ZIP file")
	}
	entries := make(map[string][]byte, len(zr.File))
	var extracted int64
	for _, file := range zr.File {
		if !safeArchivePath(file.Name) || file.FileInfo().IsDir() || file.FileInfo().Mode()&os.ModeSymlink != 0 {
			return nil, packageError("WFPKG_INVALID_ARCHIVE", "the workflow package contains unsafe file paths")
		}
		if _, exists := entries[file.Name]; exists {
			return nil, packageError("WFPKG_INVALID_ARCHIVE", "the workflow package contains duplicate files")
		}
		if file.UncompressedSize64 > MaxExtractedBytes || extracted+int64(file.UncompressedSize64) > MaxExtractedBytes {
			return nil, packageError("WFPKG_INVALID_ARCHIVE", "the workflow package exceeds the size limit after extraction")
		}
		reader, err := file.Open()
		if err != nil {
			return nil, packageError("WFPKG_INVALID_ARCHIVE", "failed to read the workflow package file")
		}
		data, readErr := io.ReadAll(io.LimitReader(reader, int64(MaxExtractedBytes)-extracted+1))
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil || len(data) > MaxExtractedBytes-int(extracted) {
			return nil, packageError("WFPKG_INVALID_ARCHIVE", "the workflow package exceeds the size limit after extraction")
		}
		extracted += int64(len(data))
		entries[file.Name] = data
	}

	manifestRaw, hasManifest := entries["manifest.json"]
	checksumsRaw, hasChecksums := entries["checksums.sha256"]
	if !hasManifest || !hasChecksums {
		return nil, packageError("WFPKG_UNSUPPORTED_FORMAT", "the workflow package is missing required files")
	}
	manifest, err := parseManifest(manifestRaw)
	if err != nil {
		return nil, err
	}
	if len(manifest.Items) != 1 || manifest.Items[0].Type != "workflow" {
		return nil, packageError("WFPKG_MULTIPLE_WORKFLOWS", "the workflow package must contain exactly one workflow")
	}
	item := manifest.Items[0]
	workflowRaw, exists := entries[item.Path]
	if !exists || !safeWorkflowPath(item.Path) || len(entries) != 3 {
		return nil, packageError("WFPKG_INVALID_ARCHIVE", "the workflow package contains undeclared files")
	}
	checksums, err := parseChecksums(checksumsRaw)
	if err != nil {
		return nil, err
	}
	if len(checksums) != 2 || checksums["manifest.json"] != sha256Prefixed(manifestRaw) || checksums[item.Path] != sha256Prefixed(workflowRaw) {
		return nil, packageError("WFPKG_CHECKSUM_MISMATCH", "workflow package checksum mismatch")
	}
	if item.ContentHash != sha256Prefixed(workflowRaw) || !validHash(item.ContentHash) || !validHash(item.GraphHash) {
		return nil, packageError("WFPKG_CHECKSUM_MISMATCH", "workflow package content checksum mismatch")
	}
	doc, err := parseDocument(workflowRaw)
	if err != nil {
		return nil, err
	}
	if !safePackageWorkflowID(doc.ID) || doc.ID != item.SourceID || doc.Version != item.SourceRevision {
		return nil, packageError("WFPKG_INVALID_MANIFEST", "workflow package manifest does not match the workflow content")
	}
	graph, err := canonicalJSON([]byte(doc.GraphJSON))
	if err != nil || item.GraphHash != sha256Prefixed(graph) {
		return nil, packageError("WFPKG_CHECKSUM_MISMATCH", "workflow graph checksum mismatch")
	}
	if validateGraph == nil || validateGraph(ctx, string(graph)) != nil {
		return nil, packageError("WFPKG_WORKFLOW_INVALID", "workflow graph failed validation")
	}
	var graphShape struct {
		Nodes []json.RawMessage `json:"nodes"`
		Edges []json.RawMessage `json:"edges"`
	}
	if err := json.Unmarshal(graph, &graphShape); err != nil {
		return nil, packageError("WFPKG_WORKFLOW_INVALID", "the workflow graph is not valid JSON")
	}
	return &InspectionResult{
		PackageHash: sha256Prefixed(archive),
		Manifest:    manifest,
		Document:    doc,
		ContentHash: item.ContentHash,
		GraphHash:   item.GraphHash,
		NodeCount:   len(graphShape.Nodes),
		EdgeCount:   len(graphShape.Edges),
	}, nil
}

func safeArchivePath(name string) bool {
	return name != "" && !strings.Contains(name, `\`) && !strings.HasPrefix(name, "/") && path.Clean(name) == name && !strings.HasPrefix(name, "../") && name != ".."
}

func safeWorkflowPath(name string) bool {
	rest := strings.TrimPrefix(name, "workflows/")
	return safeArchivePath(name) && strings.HasPrefix(name, "workflows/") && rest != "" && !strings.Contains(rest, "/") && strings.HasSuffix(rest, ".json")
}

func safePackageWorkflowID(id string) bool {
	if id == "" || strings.ContainsAny(id, `/\`) {
		return false
	}
	for _, r := range id {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func parseManifest(raw []byte) (Manifest, error) {
	var manifest Manifest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&manifest); err != nil {
		return Manifest{}, packageError("WFPKG_INVALID_MANIFEST", "workflow package manifest format is invalid")
	}
	if err := consumeJSONEnd(dec); err != nil || manifest.PackageFormat != PackageFormat || manifest.FormatVersion != FormatVersion || strings.TrimSpace(manifest.PackageID) == "" || len(manifest.Items) == 0 {
		return Manifest{}, packageError("WFPKG_INVALID_MANIFEST", "workflow package manifest format is not supported")
	}
	return manifest, nil
}

func parseDocument(raw []byte) (Document, error) {
	var doc Document
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil || consumeJSONEnd(dec) != nil {
		return Document{}, packageError("WFPKG_WORKFLOW_INVALID", "workflow definition format is invalid")
	}
	doc.ID = strings.TrimSpace(doc.ID)
	doc.Name = strings.TrimSpace(doc.Name)
	if doc.ID == "" || doc.Name == "" || doc.Version <= 0 || strings.TrimSpace(doc.GraphJSON) == "" {
		return Document{}, packageError("WFPKG_WORKFLOW_INVALID", "workflow definition is missing required fields")
	}
	return doc, nil
}

func consumeJSONEnd(dec *json.Decoder) error {
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func parseChecksums(raw []byte) (map[string]string, error) {
	entries := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "  ", 2)
		if len(parts) != 2 || !validHash("sha256:"+parts[0]) || !safeArchivePath(parts[1]) {
			return nil, packageError("WFPKG_CHECKSUM_MISMATCH", "workflow package checksum format is invalid")
		}
		if _, exists := entries[parts[1]]; exists {
			return nil, packageError("WFPKG_CHECKSUM_MISMATCH", "workflow package checksum is duplicated")
		}
		entries[parts[1]] = "sha256:" + parts[0]
	}
	return entries, nil
}
