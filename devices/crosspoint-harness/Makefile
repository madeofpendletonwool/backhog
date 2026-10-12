# CrossPoint's own OPDS parser and document-id code, built for the host
# (MAD-833). See README.md.
CROSSPOINT_REPO   ?= https://github.com/crosspoint-reader/crosspoint-reader
CROSSPOINT_COMMIT ?= e54c00873189ca01a2956ee5e911f49c91ceaa72
CP  := build/crosspoint
CXX ?= c++
CXXFLAGS ?= -std=c++20 -O1 -Wno-deprecated-declarations
LDLIBS_MD5 := $(if $(filter Darwin,$(shell uname)),,-lcrypto)

all: build/cpdocid build/cpopds

$(CP)/.fetched:
	rm -rf $(CP) && mkdir -p $(CP)
	git -C $(CP) init -q
	git -C $(CP) fetch -q --depth 1 $(CROSSPOINT_REPO) $(CROSSPOINT_COMMIT)
	git -C $(CP) checkout -q FETCH_HEAD
	touch $@

build/cpdocid: docid.cpp $(CP)/.fetched
	$(CXX) $(CXXFLAGS) -Istubs -I$(CP)/lib/KOReaderSync -I$(CP)/lib/Utf8 -I$(CP)/src -I$(CP)/src/util \
	  docid.cpp $(CP)/lib/KOReaderSync/KOReaderDocumentId.cpp $(CP)/src/util/OpdsFilename.cpp \
	  $(CP)/src/util/StringUtils.cpp $(CP)/lib/Utf8/Utf8.cpp -o $@ $(LDLIBS_MD5)

build/cpopds: opds.cpp $(CP)/.fetched
	$(CXX) $(CXXFLAGS) -Istubs -I$(CP)/lib/OpdsParser -I$(CP)/lib/XmlParserUtils \
	  opds.cpp $(CP)/lib/OpdsParser/OpdsParser.cpp -o $@ -lexpat

# Walk a live catalog through the device's parser:
#   make check-catalog URL=https://host/api/opds USER=me TOKEN=bh_...
check-catalog: build/cpopds
	../../scripts/ereader/check-catalog.sh "$(URL)" "$(USER)" "$(TOKEN)" build/cpopds

clean:
	rm -rf build

.PHONY: all clean check-catalog
