#pragma once
#include <cstdio>
#include <string>
// The SD-card file API KOReaderDocumentId reads through, over stdio.
struct HalFile {
  FILE* f = nullptr;
  ~HalFile() { if (f) fclose(f); }
  size_t fileSize() { long c = ftell(f); fseek(f, 0, SEEK_END); long s = ftell(f); fseek(f, c, SEEK_SET); return (size_t)s; }
  bool seekSet(size_t o) { return fseek(f, (long)o, SEEK_SET) == 0; }
  size_t read(unsigned char* b, size_t n) { return fread(b, 1, n, f); }
};
struct StorageT {
  bool openFileForRead(const char*, const std::string& p, HalFile& h) { h.f = fopen(p.c_str(), "rb"); return h.f != nullptr; }
};
inline StorageT Storage;
