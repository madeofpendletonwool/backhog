#!/usr/bin/env python3
"""Resolve a KOReader XPointer the way an XML-parsing engine does (MAD-833).

    scripts/ereader/xpresolve.py book.epub '/body/DocFragment[26]/body/div/section/p[3]/text()[2].227'

Prints the text at the pointer. crengine builds its DOM with an XML parser,
Backhog with an HTML5 one; this is the independent ground truth used to
check Backhog's Locator in both directions (see TestXPointerDeviceGoldens).
"""
import sys, re, zipfile, posixpath, xml.etree.ElementTree as ET
epub, xp = sys.argv[1], sys.argv[2]
z = zipfile.ZipFile(epub)
cont = ET.fromstring(z.read("META-INF/container.xml"))
opf = [e.get("full-path") for e in cont.iter() if e.tag.endswith("rootfile")][0]
pkg = ET.fromstring(z.read(opf))
man = {i.get("id"): i.get("href") for i in pkg.iter() if i.tag.endswith("}item")}
spine = [i.get("idref") for i in pkg.iter() if i.tag.endswith("itemref")]
m = re.match(r"/body/DocFragment\[(\d+)\]/body(.*)", xp)
href = posixpath.normpath(posixpath.join(posixpath.dirname(opf), man[spine[int(m.group(1)) - 1]]))
root = ET.fromstring(z.read(href))
local = lambda t: t.split("}")[-1]
cur = [e for e in root if local(e.tag) == "body"][0]
off = 0
for step in m.group(2).strip("/").split("/"):
    sm = re.match(r"([\w()]+)(?:\[(\d+)\])?(?:\.(\d+))?$", step)
    name, idx = sm.group(1), int(sm.group(2) or 1)
    if sm.group(3): off = int(sm.group(3))
    if name == "text()":
        texts = [cur.text] + [c.tail for c in cur]
        texts = [t for t in texts if t and t.strip()]
        print(texts[idx - 1][off:off + 70].replace("\n", " ")); break
    cur = [c for c in cur if local(c.tag) == name][idx - 1]
else:
    print("".join(cur.itertext())[:70].replace("\n", " "))
