package artifact

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/adk/v2/artifact"
	"google.golang.org/genai"
)

// LocalFileService implements artifact.Service storing artifacts in the local filesystem.
type LocalFileService struct {
	baseDir string
	mu      sync.Mutex
}

// NewLocalFileService initializes a local filesystem-based artifact service.
func NewLocalFileService(baseDir string) (*LocalFileService, error) {
	artifactsDir := filepath.Join(baseDir, "artifacts")
	if err := os.MkdirAll(artifactsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create artifacts directory: %w", err)
	}
	return &LocalFileService{baseDir: artifactsDir}, nil
}

// getDir returns the absolute folder path for a specific artifact.
func (s *LocalFileService) getDir(appName, userID, sessionID, fileName string) string {
	return filepath.Join(s.baseDir, appName, userID, sessionID, fileName)
}

// getSessionDir returns the folder path containing all artifacts in a session.
func (s *LocalFileService) getSessionDir(appName, userID, sessionID string) string {
	return filepath.Join(s.baseDir, appName, userID, sessionID)
}

// getLatestVersion scans the directory to find the highest version number.
func (s *LocalFileService) getLatestVersion(dir string) (int64, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	var maxVersion int64 = 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".data") {
			vStr := strings.TrimSuffix(name, ".data")
			v, err := strconv.ParseInt(vStr, 10, 64)
			if err == nil && v > maxVersion {
				maxVersion = v
			}
		}
	}
	return maxVersion, nil
}

func (s *LocalFileService) Save(ctx context.Context, req *artifact.SaveRequest) (*artifact.SaveResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := req.Validate(); err != nil {
		return nil, err
	}

	dir := s.getDir(req.AppName, req.UserID, req.SessionID, req.FileName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create version directory: %w", err)
	}

	// Resolve version
	version := req.Version
	if version == 0 {
		latest, err := s.getLatestVersion(dir)
		if err != nil {
			return nil, fmt.Errorf("failed to read latest version: %w", err)
		}
		version = latest + 1
	}

	// Write Part data
	partBytes, err := json.Marshal(req.Part)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize genai.Part: %w", err)
	}

	dataPath := filepath.Join(dir, fmt.Sprintf("%d.data", version))
	if err := os.WriteFile(dataPath, partBytes, 0644); err != nil {
		return nil, fmt.Errorf("failed to save data file: %w", err)
	}

	// Write metadata file
	meta := &artifact.ArtifactVersion{
		Version:      version,
		CanonicalURI: fmt.Sprintf("file://%s", filepath.ToSlash(dataPath)),
		CreateTime:   time.Now(),
		MimeType:     "text/plain",
	}

	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize metadata: %w", err)
	}

	metaPath := filepath.Join(dir, fmt.Sprintf("%d.meta", version))
	if err := os.WriteFile(metaPath, metaBytes, 0644); err != nil {
		return nil, fmt.Errorf("failed to save metadata file: %w", err)
	}

	return &artifact.SaveResponse{
		Version: version,
	}, nil
}

func (s *LocalFileService) Load(ctx context.Context, req *artifact.LoadRequest) (*artifact.LoadResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := req.Validate(); err != nil {
		return nil, err
	}

	dir := s.getDir(req.AppName, req.UserID, req.SessionID, req.FileName)
	version := req.Version
	if version == 0 {
		latest, err := s.getLatestVersion(dir)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve latest version: %w", err)
		}
		if latest == 0 {
			return nil, fmt.Errorf("no versions found for artifact: %s", req.FileName)
		}
		version = latest
	}

	dataPath := filepath.Join(dir, fmt.Sprintf("%d.data", version))
	partBytes, err := os.ReadFile(dataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read data file: %w", err)
	}

	var part genai.Part
	if err := json.Unmarshal(partBytes, &part); err != nil {
		return nil, fmt.Errorf("failed to parse genai.Part: %w", err)
	}

	return &artifact.LoadResponse{
		Part: &part,
	}, nil
}

func (s *LocalFileService) Delete(ctx context.Context, req *artifact.DeleteRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := req.Validate(); err != nil {
		return err
	}

	dir := s.getDir(req.AppName, req.UserID, req.SessionID, req.FileName)
	if req.Version != 0 {
		// Delete specific version files
		dataPath := filepath.Join(dir, fmt.Sprintf("%d.data", req.Version))
		metaPath := filepath.Join(dir, fmt.Sprintf("%d.meta", req.Version))
		_ = os.Remove(dataPath)
		_ = os.Remove(metaPath)
		return nil
	}

	// Delete entire directory
	return os.RemoveAll(dir)
}

func (s *LocalFileService) List(ctx context.Context, req *artifact.ListRequest) (*artifact.ListResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := req.Validate(); err != nil {
		return nil, err
	}

	sessionDir := s.getSessionDir(req.AppName, req.UserID, req.SessionID)
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		if os.IsNotExist(err) {
			return &artifact.ListResponse{FileNames: []string{}}, nil
		}
		return nil, err
	}

	var fileNames []string
	for _, entry := range entries {
		if entry.IsDir() {
			fileNames = append(fileNames, entry.Name())
		}
	}

	return &artifact.ListResponse{
		FileNames: fileNames,
	}, nil
}

func (s *LocalFileService) Versions(ctx context.Context, req *artifact.VersionsRequest) (*artifact.VersionsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := req.Validate(); err != nil {
		return nil, err
	}

	dir := s.getDir(req.AppName, req.UserID, req.SessionID, req.FileName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return &artifact.VersionsResponse{Versions: []int64{}}, nil
		}
		return nil, err
	}

	var versions []int64
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".data") {
			vStr := strings.TrimSuffix(name, ".data")
			v, err := strconv.ParseInt(vStr, 10, 64)
			if err == nil {
				versions = append(versions, v)
			}
		}
	}

	// Sort versions ascending
	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })

	return &artifact.VersionsResponse{
		Versions: versions,
	}, nil
}

func (s *LocalFileService) GetArtifactVersion(ctx context.Context, req *artifact.GetArtifactVersionRequest) (*artifact.GetArtifactVersionResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := req.Validate(); err != nil {
		return nil, err
	}

	dir := s.getDir(req.AppName, req.UserID, req.SessionID, req.FileName)
	version := req.Version
	if version == 0 {
		latest, err := s.getLatestVersion(dir)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve latest version: %w", err)
		}
		if latest == 0 {
			return nil, fmt.Errorf("no versions found for artifact: %s", req.FileName)
		}
		version = latest
	}

	metaPath := filepath.Join(dir, fmt.Sprintf("%d.meta", version))
	metaBytes, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read metadata file: %w", err)
	}

	var meta artifact.ArtifactVersion
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	return &artifact.GetArtifactVersionResponse{
		ArtifactVersion: &meta,
	}, nil
}
