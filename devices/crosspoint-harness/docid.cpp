// CrossPoint's own document-id and download-filename code, on the host.
//   docid <file>                      KOReader partial-MD5 of a file
//   name <format 0|1|2> <author> <title>
//                                     the filename CrossPoint saves an OPDS
//                                     download as, and its filename-mode id
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <string>
#include "KOReaderDocumentId.h"
#include "util/OpdsFilename.h"
int main(int argc, char** argv) {
  if (argc == 3 && !strcmp(argv[1], "docid")) {
    printf("%s\n", KOReaderDocumentId::calculate(argv[2]).c_str());
    return 0;
  }
  if (argc == 5 && !strcmp(argv[1], "name")) {
    std::string n = opdsBookFilename(argv[3], argv[4], static_cast<OpdsFilenameFormat>(atoi(argv[2])));
    printf("%s\t%s\n", n.c_str(), KOReaderDocumentId::calculateFromFilename("/books/" + n).c_str());
    return 0;
  }
  fprintf(stderr, "usage: docid <file> | name <0|1|2> <author> <title>\n");
  return 2;
}
