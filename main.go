package main

import (
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Shared state for output file management
type outputState struct {
	currentFile *os.File
	currentSize int64
	fileIndex   int
	maxFileSize int64
	outputDir   string
	mutex       sync.Mutex
}

// File metadata structure
type fileMetadata struct {
	name    string
	relPath string
	size    int64
	content []byte
}

// Configuration options
type config struct {
	rootPath        string
	maxFileSizeKB   int
	outputDir       string
	skipHiddenFiles bool
	// NEW: Maps for storing items to skip for efficient lookups
	skipExts map[string]struct{}
	skipDirs map[string]struct{}
}

func main() {
	cfg := &config{}

	// NEW: String vars to capture comma-separated flag values
	var skipExtsStr, skipDirsStr string

	flag.StringVar(&cfg.rootPath, "root", ".", "Root directory to process")
	flag.IntVar(&cfg.maxFileSizeKB, "max-size", 1024, "Maximum output file size in KB")
	flag.StringVar(&cfg.outputDir, "output", "output", "Output directory for generated files")
	flag.BoolVar(&cfg.skipHiddenFiles, "skip-hidden", true, "Skip hidden files and directories (default: true)")
	// NEW: Define new command-line flags for skipping extensions and directories
	flag.StringVar(&skipExtsStr, "skip-ext", "", "Comma-separated list of file extensions to skip (e.g., .log,.tmp)")
	flag.StringVar(&skipDirsStr, "skip-dir", "", "Comma-separated list of directory names to skip (e.g., node_modules,.git)")
	flag.Parse()

	if _, err := os.Stat(cfg.rootPath); os.IsNotExist(err) {
		log.Fatalf("Root directory does not exist: %s", cfg.rootPath)
	}

	if cfg.maxFileSizeKB <= 0 {
		log.Fatal("Max file size must be positive")
	}

	// NEW: Process the string flags into maps for efficient lookup
	cfg.skipExts = make(map[string]struct{})
	if skipExtsStr != "" {
		for _, ext := range strings.Split(skipExtsStr, ",") {
			trimmedExt := strings.TrimSpace(ext)
			if !strings.HasPrefix(trimmedExt, ".") {
				trimmedExt = "." + trimmedExt
			}
			cfg.skipExts[trimmedExt] = struct{}{}
		}
	}

	cfg.skipDirs = make(map[string]struct{})
	if skipDirsStr != "" {
		for _, dir := range strings.Split(skipDirsStr, ",") {
			cfg.skipDirs[strings.TrimSpace(dir)] = struct{}{}
		}
	}

	if err := os.MkdirAll(cfg.outputDir, 0755); err != nil {
		log.Fatalf("Failed to create output directory: %v", err)
	}

	directoryTree, err := generateDirectoryTree(cfg)
	if err != nil {
		log.Fatalf("Failed to generate directory tree: %v", err)
	}

	filePaths, err := collectFilePaths(cfg)
	if err != nil {
		log.Fatalf("Failed to collect file paths: %v", err)
	}

	maxBytes := int64(cfg.maxFileSizeKB) * 1024
	state := &outputState{
		fileIndex:   1,
		maxFileSize: maxBytes,
		outputDir:   cfg.outputDir,
	}

	if err := state.createNewOutputFile(); err != nil {
		log.Fatalf("Failed to create initial output file: %v", err)
	}
	defer state.currentFile.Close()

	if _, err := state.currentFile.WriteString("DIRECTORY STRUCTURE:\n" + directoryTree + "\n\n"); err != nil {
		log.Fatalf("Failed to write directory structure: %v", err)
	}
	state.currentSize = int64(len(directoryTree)) + 2

	processFiles(filePaths, cfg, state)

	log.Println("File processing completed successfully")
}

func isHiddenFile(path string) bool {
	base := filepath.Base(path)
	return strings.HasPrefix(base, ".")
}

// generateDirectoryTree creates a string representation of the directory structure
func generateDirectoryTree(cfg *config) (string, error) {
	var builder strings.Builder
	err := filepath.Walk(cfg.rootPath, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// MODIFIED: Enhanced skip logic
		baseName := info.Name()

		// Skip hidden files and directories if configured
		if cfg.skipHiddenFiles && isHiddenFile(path) {
			if info.IsDir() {
				return filepath.SkipDir // Skip the entire directory
			}
			return nil // Skip the file
		}

		if info.IsDir() {
			// Skip specified directory names
			if _, found := cfg.skipDirs[baseName]; found {
				return filepath.SkipDir
			}
		} else {
			// Skip specified file extensions
			if _, found := cfg.skipExts[filepath.Ext(baseName)]; found {
				return nil
			}
		}

		// Calculate relative path and depth
		relPath, err := filepath.Rel(cfg.rootPath, path)
		if err != nil {
			return err
		}

		// Skip the root directory itself
		if relPath == "." {
			return nil
		}

		// Calculate depth based on the number of separators
		depth := strings.Count(relPath, string(filepath.Separator))
		indent := strings.Repeat("  ", depth)

		// Add directory or file entry
		prefix := "├── "
		if info.IsDir() {
			prefix = "└── "
		}

		builder.WriteString(indent + prefix + filepath.Base(path) + "\n")
		return nil
	})

	if err != nil {
		return "", err
	}

	return builder.String(), nil
}

// collectFilePaths gathers all file paths in the directory tree
func collectFilePaths(cfg *config) ([]string, error) {
	var filePaths []string
	err := filepath.Walk(cfg.rootPath, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// MODIFIED: Enhanced skip logic
		baseName := info.Name()

		// Skip hidden files and directories if configured
		if cfg.skipHiddenFiles && isHiddenFile(path) {
			if info.IsDir() {
				return filepath.SkipDir // Skip the entire directory
			}
			return nil // Skip the file
		}

		// If it's a directory, check if it should be skipped
		if info.IsDir() {
			if _, found := cfg.skipDirs[baseName]; found {
				return filepath.SkipDir
			}
			return nil // Continue traversal but don't add directory path to the list
		}

		// If it's a file, check its extension
		if _, found := cfg.skipExts[filepath.Ext(baseName)]; found {
			return nil // Skip this file
		}

		// If all checks pass, add the file path
		filePaths = append(filePaths, path)
		return nil
	})

	if err != nil {
		return nil, err
	}

	return filePaths, nil
}

// processFiles handles parallel file processing
func processFiles(filePaths []string, cfg *config, state *outputState) {
	var wg sync.WaitGroup
	fileChan := make(chan string, len(filePaths))

	// Create worker pool
	numWorkers := 4 // Adjust based on your system
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go worker(fileChan, &wg, cfg, state)
	}

	// Send file paths to workers
	for _, path := range filePaths {
		fileChan <- path
	}
	close(fileChan)

	wg.Wait()
}

// worker processes files from the channel
func worker(fileChan <-chan string, wg *sync.WaitGroup, cfg *config, state *outputState) {
	defer wg.Done()

	for filePath := range fileChan {
		processFile(filePath, cfg, state)
	}
}

// processFile reads a file and writes its content to the output
func processFile(filePath string, cfg *config, state *outputState) {
	// Note: An explicit check here is redundant because collectFilePaths
	// already filters the list, but it's kept for robustness.
	if cfg.skipHiddenFiles && isHiddenFile(filePath) {
		return
	}

	// Read file content
	content, err := os.ReadFile(filePath)
	if err != nil {
		log.Printf("Error reading file %s: %v", filePath, err)
		return
	}

	// Get file info for metadata
	info, err := os.Stat(filePath)
	if err != nil {
		log.Printf("Error getting file info for %s: %v", filePath, err)
		return
	}

	// Calculate relative path
	relPath, err := filepath.Rel(cfg.rootPath, filePath)
	if err != nil {
		log.Printf("Error calculating relative path for %s: %v", filePath, err)
		return
	}

	// Create metadata
	metadata := fileMetadata{
		name:    info.Name(),
		relPath: relPath,
		size:    info.Size(),
		content: content,
	}

	// Write to output file
	if err := state.writeFileWithMetadata(metadata); err != nil {
		log.Printf("Error writing file %s to output: %v", filePath, err)
	}
}

// writeFileWithMetadata writes file content with metadata to the current output file
func (s *outputState) writeFileWithMetadata(metadata fileMetadata) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Format the metadata header
	header := fmt.Sprintf(
		"File: %s\nPath: %s\nSize: %d bytes\nFILE CONTENT START:\n",
		metadata.name,
		metadata.relPath,
		metadata.size,
	)
	footer := "\nFILE CONTENT END\n\n"

	// Calculate total size needed
	totalSize := int64(len(header)) + metadata.size + int64(len(footer))

	// Check if we need a new output file
	if s.currentSize+totalSize > s.maxFileSize && s.currentSize > 0 {
		if err := s.createNewOutputFile(); err != nil {
			return err
		}
	}

	// Write header, content, and footer
	if _, err := s.currentFile.WriteString(header); err != nil {
		return err
	}
	if _, err := s.currentFile.Write(metadata.content); err != nil {
		return err
	}
	if _, err := s.currentFile.WriteString(footer); err != nil {
		return err
	}

	// Update current size
	s.currentSize += totalSize
	return nil
}

// createNewOutputFile closes the current file and creates a new one
func (s *outputState) createNewOutputFile() error {
	// Close current file if it exists
	if s.currentFile != nil {
		if err := s.currentFile.Close(); err != nil {
			return err
		}
	}

	// Create new output file
	fileName := filepath.Join(s.outputDir, fmt.Sprintf("output_%03d.txt", s.fileIndex))
	file, err := os.Create(fileName)
	if err != nil {
		return err
	}

	s.currentFile = file
	s.fileIndex++
	s.currentSize = 0
	return nil
}