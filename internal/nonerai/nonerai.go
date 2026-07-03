// Package nonerai converts NEIZZIR (Black Russia encrypted) .zip packages
// into .nonerai (Black Russia standard) packages.
//
// Original Python (main.py:1755-1794):
//
//      1. Extract source .zip to a "raw" directory.
//      2. Find the NEIZZIR/ subfolder.
//      3. Build an output structure:
//         build/Assembly/{anim,data,fonts}/
//         build/Assembly/dynamic/{br_tex_nonerai.astc.bpc, br_nonerai.bpc}
//         build/Assembly/audio/samples/GENERIC.bpc + GENERIC.bpcmeta
//         build/Audio/hit_1.mp3, hit_2.mp3, hit_3.mp3
//      4. Generate GENERIC.bpcmeta via generate_bpcmeta (binary format).
//      5. Zip the build/ folder into a .nonerai file.
//
// Bug fixes vs Python:
//   - Python's `convert_zip2nonerai` returned `None` if NEIZZIR wasn't found
//     (via `return print(...)` — print returns None). We return an explicit
//     error instead.
//   - Python hardcoded "GENERIC.bpc" as the source for the gen_temp extraction;
//     if the file didn't exist, the zip.open would crash. We check first.
//   - Python's file move logic used shutil.move which can fail silently on
//     cross-device moves. We use io.Copy + os.Remove.
package nonerai

import (
        "archive/zip"
        "bytes"
        "encoding/binary"
        "fmt"
        "io"
        "os"
        "path/filepath"
        "strings"
)

// Convert takes a source .zip (NEIZZIR format) and produces a .nonerai
// (standard format) at outPath.
//
// genrlBpcData is the GENERIC.bpc template bytes (from the assets folder).
// If nil, the function returns an error — the conversion needs this file
// to build the audio samples.
func Convert(srcZipPath, outPath string, genrlBpcData []byte) error {
        if len(genrlBpcData) == 0 {
                return fmt.Errorf("nonerai: genrlBpcData is empty (need GENERIC.bpc template)")
        }

        tmpDir, err := os.MkdirTemp("", "nonerai-*")
        if err != nil {
                return fmt.Errorf("nonerai: mktemp: %w", err)
        }
        defer os.RemoveAll(tmpDir)

        // 1. Extract source .zip to tmpDir/raw.
        rawDir := filepath.Join(tmpDir, "raw")
        if err := os.MkdirAll(rawDir, 0o755); err != nil {
                return err
        }
        if err := extractZip(srcZipPath, rawDir); err != nil {
                return fmt.Errorf("nonerai: extract source: %w", err)
        }

        // 2. Find NEIZZIR/ subfolder.
        var nezDir string
        err = filepath.Walk(rawDir, func(path string, info os.FileInfo, err error) error {
                if err != nil {
                        return err
                }
                if info.IsDir() && info.Name() == "NEIZZIR" {
                        nezDir = path
                        return filepath.SkipDir
                }
                return nil
        })
        if err != nil {
                return err
        }
        if nezDir == "" {
                return fmt.Errorf("nonerai: NEIZZIR folder not found in source zip")
        }

        // 3. Build the output structure.
        buildDir := filepath.Join(tmpDir, "build")
        asmDir := filepath.Join(buildDir, "Assembly")
        dynDir := filepath.Join(asmDir, "dynamic")
        audioInSamplesDir := filepath.Join(asmDir, "audio/samples")
        audioOutDir := filepath.Join(buildDir, "Audio")
        for _, d := range []string{dynDir, audioInSamplesDir, audioOutDir} {
                if err := os.MkdirAll(d, 0o755); err != nil {
                        return err
                }
        }

        // Move anim/, data/, fonts/ from NEIZZIR/ to Assembly/.
        for _, fld := range []string{"anim", "data", "fonts"} {
                src := filepath.Join(nezDir, fld)
                if _, err := os.Stat(src); err == nil {
                        dst := filepath.Join(asmDir, fld)
                        if err := os.Rename(src, dst); err != nil {
                                return fmt.Errorf("nonerai: move %s: %w", fld, err)
                        }
                }
        }

        // Copy sound_1..3.mp3 to Audio/hit_1..3.mp3.
        for i := 1; i <= 3; i++ {
                src := filepath.Join(nezDir, fmt.Sprintf("sound_%d.mp3", i))
                if _, err := os.Stat(src); err == nil {
                        dst := filepath.Join(audioOutDir, fmt.Sprintf("hit_%d.mp3", i))
                        if err := copyFile(src, dst); err != nil {
                                return err
                        }
                }
        }

        // Build GENERIC.bpc by extracting genrlBpcData, then merging NEIZZIR/GENRL/.
        genTemp := filepath.Join(tmpDir, "gen_work")
        if err := os.MkdirAll(genTemp, 0o755); err != nil {
                return err
        }
        if err := extractZipFromBytes(genrlBpcData, genTemp); err != nil {
                return fmt.Errorf("nonerai: extract GENERIC.bpc: %w", err)
        }
        // Merge NEIZZIR/GENRL/ into genTemp (overrides existing files).
        nezGenrl := filepath.Join(nezDir, "GENRL")
        if _, err := os.Stat(nezGenrl); err == nil {
                if err := mergeDirs(nezGenrl, genTemp); err != nil {
                        return err
                }
        }

        // Pack genTemp into audio/samples/GENERIC.bpc.
        genBpcPath := filepath.Join(audioInSamplesDir, "GENERIC.bpc")
        if err := zipDirectory(genTemp, genBpcPath); err != nil {
                return fmt.Errorf("nonerai: pack GENERIC.bpc: %w", err)
        }

        // Generate GENERIC.bpcmeta.
        bpcmetaPath := filepath.Join(audioInSamplesDir, "GENERIC.bpcmeta")
        if err := generateBpcmeta(genBpcPath, bpcmetaPath); err != nil {
                return fmt.Errorf("nonerai: generate bpcmeta: %w", err)
        }

        // Move .astc.bpc and other .bpc files into dynamic/.
        entries, err := os.ReadDir(nezDir)
        if err != nil {
                return err
        }
        for _, e := range entries {
                if e.IsDir() {
                        continue
                }
                name := e.Name()
                lower := strings.ToLower(name)
                if strings.HasSuffix(lower, ".zip") && strings.Contains(lower, ".astc") {
                        src := filepath.Join(nezDir, name)
                        dst := filepath.Join(dynDir, "br_tex_nonerai.astc.bpc")
                        if err := copyFile(src, dst); err != nil {
                                return err
                        }
                } else if strings.HasSuffix(lower, ".bpc") && !strings.Contains(lower, "GENRL") {
                        src := filepath.Join(nezDir, name)
                        dst := filepath.Join(dynDir, "br_nonerai.bpc")
                        if err := copyFile(src, dst); err != nil {
                                return err
                        }
                }
        }

        // 4. Zip the build/ folder into the output .nonerai.
        if err := zipDirectory(buildDir, outPath); err != nil {
                return fmt.Errorf("nonerai: pack output: %w", err)
        }
        return nil
}

// ── helpers ───────────────────────────────────────────────────────────────

func extractZip(srcPath, dstDir string) error {
        r, err := zip.OpenReader(srcPath)
        if err != nil {
                return err
        }
        defer r.Close()
        return extractZipReader(&r.Reader, dstDir)
}

func extractZipFromBytes(data []byte, dstDir string) error {
        r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
        if err != nil {
                return err
        }
        return extractZipReader(r, dstDir)
}

func extractZipReader(r *zip.Reader, dstDir string) error {
        for _, f := range r.File {
                path := filepath.Join(dstDir, f.Name)
                if !strings.HasPrefix(filepath.Clean(path)+string(os.PathSeparator), filepath.Clean(dstDir)+string(os.PathSeparator)) {
                        return fmt.Errorf("zip slip: %q escapes %q", f.Name, dstDir)
                }
                if f.FileInfo().IsDir() {
                        if err := os.MkdirAll(path, 0o755); err != nil {
                                return err
                        }
                        continue
                }
                if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
                        return err
                }
                out, err := os.Create(path)
                if err != nil {
                        return err
                }
                rc, err := f.Open()
                if err != nil {
                        out.Close()
                        return err
                }
                _, err = io.Copy(out, rc)
                rc.Close()
                out.Close()
                if err != nil {
                        return err
                }
        }
        return nil
}

func copyFile(src, dst string) error {
        in, err := os.Open(src)
        if err != nil {
                return err
        }
        defer in.Close()
        out, err := os.Create(dst)
        if err != nil {
                return err
        }
        defer out.Close()
        _, err = io.Copy(out, in)
        return err
}

func mergeDirs(src, dst string) error {
        return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
                if err != nil {
                        return err
                }
                rel, err := filepath.Rel(src, path)
                if err != nil {
                        return err
                }
                dstPath := filepath.Join(dst, rel)
                if info.IsDir() {
                        return os.MkdirAll(dstPath, 0o755)
                }
                return copyFile(path, dstPath)
        })
}

func zipDirectory(srcDir, outPath string) error {
        out, err := os.Create(outPath)
        if err != nil {
                return err
        }
        defer out.Close()
        zw := zip.NewWriter(out)
        defer zw.Close()

        return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
                if err != nil {
                        return err
                }
                if info.IsDir() {
                        return nil
                }
                rel, err := filepath.Rel(srcDir, path)
                if err != nil {
                        return err
                }
                hdr, err := zip.FileInfoHeader(info)
                if err != nil {
                        return err
                }
                hdr.Name = filepath.ToSlash(rel)
                hdr.Method = zip.Store
                w, err := zw.CreateHeader(hdr)
                if err != nil {
                        return err
                }
                in, err := os.Open(path)
                if err != nil {
                        return err
                }
                defer in.Close()
                _, err = io.Copy(w, in)
                return err
        })
}

// generateBpcmeta creates the binary .bpcmeta file for an audio .bpc archive.
//
// Format (matches Python generate_bpcmeta):
//   - uint32: entry count
//   - per entry:
//     - uint32: data_offset (offset of the file's content in the .bpc)
//     - uint32: comp_size (compressed size of the file)
//     - uint8:  is_mp3 (1 if .mp3, 0 otherwise)
//     - uint16: name_len
//     - bytes:  name (UTF-8)
//
// Entries are sorted by name (case-insensitive).
//
// data_offset is computed from the .bpc's local file header (zip's
// header_offset + 30 + len(filename) + len(extra)).
func generateBpcmeta(bpcPath, outPath string) error {
        r, err := zip.OpenReader(bpcPath)
        if err != nil {
                return err
        }
        defer r.Close()

        type entry struct {
                name       string
                dataOffset uint32
                compSize   uint32
                isMP3      bool
        }
        var entries []entry
        for _, f := range r.File {
                lower := strings.ToLower(f.Name)
                if !strings.HasSuffix(lower, ".mp3") && !strings.HasSuffix(lower, ".wav") && !strings.HasSuffix(lower, ".ogg") {
                        continue
                }
                // DataOffset returns the offset of the file's actual data (after
                // the local file header + name + extra field).
                dataOffset, err := f.DataOffset()
                if err != nil {
                        return fmt.Errorf("nonerai: data offset for %s: %w", f.Name, err)
                }
                entries = append(entries, entry{
                        name:       f.Name,
                        dataOffset: uint32(dataOffset),
                        compSize:   uint32(f.CompressedSize64),
                        isMP3:      strings.HasSuffix(lower, ".mp3"),
                })
        }

        // Sort entries by name (case-insensitive).
        for i := 0; i < len(entries); i++ {
                for j := i + 1; j < len(entries); j++ {
                        if strings.ToLower(entries[j].name) < strings.ToLower(entries[i].name) {
                                entries[i], entries[j] = entries[j], entries[i]
                        }
                }
        }

        // Write the .bpcmeta.
        var buf bytes.Buffer
        binary.Write(&buf, binary.LittleEndian, uint32(len(entries)))
        for _, e := range entries {
                binary.Write(&buf, binary.LittleEndian, e.dataOffset)
                binary.Write(&buf, binary.LittleEndian, e.compSize)
                isMP3 := uint8(0)
                if e.isMP3 {
                        isMP3 = 1
                }
                buf.WriteByte(isMP3)
                nameBytes := []byte(e.name)
                binary.Write(&buf, binary.LittleEndian, uint16(len(nameBytes)))
                buf.Write(nameBytes)
        }
        return os.WriteFile(outPath, buf.Bytes(), 0o644)
}
