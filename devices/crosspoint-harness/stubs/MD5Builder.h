#pragma once
#include <cstdio>
#include <cstring>
#include <string>
#ifdef __APPLE__
#define COMMON_DIGEST_FOR_OPENSSL
#include <CommonCrypto/CommonDigest.h>
#else
#include <openssl/md5.h>
#endif
// Arduino's MD5Builder over the platform MD5.
struct HexString { std::string s; const char* c_str() const { return s.c_str(); } };
class MD5Builder {
  MD5_CTX ctx; unsigned char d[16];
 public:
  void begin() { MD5_Init(&ctx); }
  void add(const unsigned char* b, size_t n) { MD5_Update(&ctx, b, n); }
  void add(const char* s) { MD5_Update(&ctx, s, strlen(s)); }
  void calculate() { MD5_Final(d, &ctx); }
  HexString toString() { char h[33]; for (int i = 0; i < 16; i++) snprintf(h + 2 * i, 3, "%02x", d[i]); return {std::string(h, 32)}; }
};
