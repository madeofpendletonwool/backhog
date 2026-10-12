#pragma once
#include <cstddef>
#include <cstdint>
// Arduino's Print, as much of it as OpdsParser overrides.
class Print {
 public:
  virtual ~Print() = default;
  virtual size_t write(uint8_t) = 0;
  virtual size_t write(const uint8_t* b, size_t n) = 0;
  virtual void flush() {}
};
