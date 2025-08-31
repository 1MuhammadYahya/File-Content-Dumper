### README

**File packer: directory tree + chunked file dump**

This tool walks a directory, prints a tree of everything it finds, and then streams file contents into one or more output text files, automatically splitting when a max size is reached. Processing is concurrent for speed and **optionally skips hidden files, specific directory names, and files with certain extensions.**

-----

### Features

  - Generates an ASCII directory tree for the root path.
  - Collects every regular file path under the root.
  - Reads files and writes them into output text files with per-file metadata headers and footers.
  - Splits output into multiple files when a size threshold is reached.
  - Concurrent file processing with a worker pool.
  - Optionally skips hidden files and directories starting with a dot.
  - **NEW: Skips user-defined directory names (e.g., `node_modules`, `.git`).**
  - **NEW: Skips files with user-defined extensions (e.g., `.log`, `.tmp`).**

-----

### How it works

1.  Parse CLI flags into config.
2.  Validate the root exists and prepare the output directory.
3.  Build a directory tree string using `filepath.Walk`, **honoring all skip rules (hidden, by name, by extension).**
4.  Collect all file paths using `filepath.Walk`, **honoring the same skip rules.**
5.  Initialize an `outputState` which manages:
      - Current output file handle
      - Current file size and rolling index
      - Max file size in bytes
      - A mutex to serialize writes from workers
6.  Write the directory tree at the top of the first output file.
7.  Start a fixed-size worker pool that:
      - Reads each file
      - Computes metadata
      - Calls `writeFileWithMetadata`, which locks, checks remaining space, rotates files if needed, and writes header, content, and footer.

-----

### Build and install

  - Prerequisites: Go 1.20+ recommended
  - Build:
      - `go build -o file-packer`
  - Install to `$GOPATH/bin`:
      - `go install`

-----

### Usage

  - **Basic**

      - `./file-packer`
      - Walks `.` and writes to `./output/output_001.txt`, splitting at 1 MB.

  - **Choose a root directory**

      - `./file-packer -root ./my-project`

  - **Increase max output file size (KB)**

      - `./file-packer -max-size 8192`

  - **Change output directory**

      - `./file-packer -output ./artifacts`

  - **Include hidden files and directories**

      - `./file-packer -skip-hidden=false`

  - **NEW: Skip specific directories**

      - `./file-packer -skip-dir .git,node_modules,vendor`

  - **NEW: Skip specific file extensions**

      - `./file-packer -skip-ext .log,.tmp,.bak`

-----

### Flags

  - `-root string`  
    Root directory to process. **Default: "."**
  - `-max-size int`  
    Maximum output file size in KB. **Default: 1024**
  - `-output string`  
    Output directory for generated files. **Default: "output"**
  - `-skip-hidden bool`  
    Skip hidden files and directories that start with ".". **Default: true**
  - **`-skip-ext string`** **Comma-separated list of file extensions to skip (e.g., .log,.tmp).**
  - **`-skip-dir string`** **Comma-separated list of directory names to skip (e.g., node\_modules,.git).**

-----

### Output

  - Files: `output/output_001.txt`, `output/output_002.txt`, ...
  - Header + content + footer per file, for example:

<!-- end list -->

```
DIRECTORY STRUCTURE:
└── cmd
  ├── main.go
  └── util.go

File: main.go
Path: cmd/main.go
Size: 1234 bytes
FILE CONTENT START:
...file bytes...
FILE CONTENT END
```

  - **Rotation**: When the next write would exceed max size, the writer closes the current file and opens `output_NNN+1.txt`. Writes are mutex-protected.

**Notes**:

  - The directory tree is written once at the very top of the first output file.
  - The code records `currentSize` based on written headers and content, but initial assignment after writing the tree uses `len(tree)+2` bytes, while the actual bytes written include the “DIRECTORY STRUCTURE:\\n” prefix. If exact byte accounting matters, consider updating `currentSize` to include this prefix length.

-----

### Concurrency

  - A fixed worker pool size of 4 is used in `processFiles`. Adjust for your environment or make it configurable.
  - Writes are serialized with a mutex inside `outputState` to ensure output file integrity and correct rotation.

-----

### Error handling

  - **Startup validation**:
      - Fails if root does not exist or `-max-size <= 0`.
      - Creates output directory if needed.
  - **During processing**:
      - Logs errors per file but continues processing others.
      - Recoverable errors do not stop the entire run.

-----

### Performance tips

  - Increase `-max-size` to reduce rotations and filesystem overhead.
  - Tune worker count in `processFiles` for your storage and CPU characteristics.
  - When processing large trees on spinning disks, consider reducing workers to reduce random I/O.

-----

### Security and safety

  - The tool reads and writes bytes as-is. No filtering or sanitizing of file content.
  - Hidden files are skipped by default to avoid dot-directories like `.git`. You can also explicitly skip such directories with `-skip-dir`. Disable with `-skip-hidden=false`.

-----

### Limitations and known behaviors

  - Binary content is written directly into text files. Viewing in terminals may be noisy. This is intended for archival rather than human reading.
  - The directory tree indentation uses two spaces per depth. Directory entries are labeled with `└──` and files with `├──`, which may not reflect tree semantics perfectly for all orders.
  - `currentSize` tracking starts at 0 for a fresh output file and increases with each write; after writing the tree, `currentSize` is set using `len(tree)+2`. If precise accounting of the preceding “DIRECTORY STRUCTURE:\\n” prefix is required, update `currentSize` accordingly.
  - Worker count is fixed at 4 in code. Make it a flag if you need flexibility.

-----

### Examples

  - **Archive a project with 16 MB chunks, including dotfiles, to `./dump`**

      - `./file-packer -root ./project -max-size 16384 -output ./dump -skip-hidden=false`

  - **Quick snapshot of current directory to default output**

      - `./file-packer`

  - **NEW: Archive a project, ignoring build artifacts and logs**

      - `./file-packer -root ./my-app -skip-dir build,dist,node_modules -skip-ext .log,.tmp`

-----

### Code structure

  - **`main`** Parses flags, validates, builds directory tree and file list, initializes state, writes tree, launches workers.
  - **`generateDirectoryTree`** Walks the root, builds an ASCII tree string, **respects all skip rules (`-skip-hidden`, `-skip-dir`, `-skip-ext`).**
  - **`collectFilePaths`** Walks the root, collects regular files, **respects all skip rules.**
  - **`processFiles`, `worker`** Bounded concurrency over files.
  - **`processFile`** Reads content, builds metadata, delegates to writer.
  - **`outputState`** Manages current file, size, index, rotation, and synchronized writes.

-----

### Possible enhancements

  - Make worker count configurable via a `-workers` flag.
  - Optional gzip compression per output file.
  - Filter patterns to include or exclude by glob.
  - Write directory tree to a separate file and link from outputs.
  - JSON index file that lists all entries and the output part they reside in.

-----

### License

MIT.

-----

### Troubleshooting

  - “Root directory does not exist”: Check `-root` path.
  - “Max file size must be positive”: Use an integer \> 0 for `-max-size`.
  - “Failed to create output directory”: Ensure you have permission or choose another `-output` path.
  - Output files very large or many parts: Adjust `-max-size` to suit your needs.