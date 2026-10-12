// Feeds an OPDS document on stdin through CrossPoint's own OpdsParser, in
// 512-byte writes like the device's HTTP stream, and prints what it saw.
#include <algorithm>
#include <cstdio>
#include <iostream>
#include <iterator>
#include <string>
#include "OpdsParser.h"
int main() {
  std::string in((std::istreambuf_iterator<char>(std::cin)), {});
  OpdsParser p;
  for (size_t i = 0; i < in.size(); i += 512)
    p.write(reinterpret_cast<const uint8_t*>(in.data() + i), std::min<size_t>(512, in.size() - i));
  p.flush();
  printf("error=%d truncated=%d entries=%zu\nsearch=%s\nnext=%s\nprev=%s\n", p.error(), p.truncated(),
         p.getEntries().size(), p.getSearchTemplate().c_str(), p.getNextPageUrl().c_str(), p.getPrevPageUrl().c_str());
  for (auto& e : p.getEntries())
    printf("%s | %s | %s | %s\n", e.type == OpdsEntryType::BOOK ? "BOOK" : "NAV ", e.title.c_str(), e.author.c_str(),
           e.href.c_str());
  return p.error() ? 1 : 0;
}
