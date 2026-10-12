#!/usr/bin/env python3
"""Diff Backhog's KOReader sync server against the reference one (MAD-833).

Sends the same request sequence a KOReader / CrossPoint device sends to both
servers and compares status codes and JSON *shapes* (keys and value types,
not values). Divergences Backhog makes on purpose are allow-listed below.

    docker run -d --name kosync-ref -p 17200:7200 koreader/kosync:latest
    scripts/ereader/kosync-diff.py \\
        --ref https://localhost:17200 \\
        --backhog http://localhost:8097/api/kosync \\
        --user collin --token bh_... --document <partial md5 of a book you own>

--document must name a book in the Backhog user's library (download it once
through the OPDS catalog, or use its KOReader partial MD5).
"""
import argparse, hashlib, json, ssl, sys, urllib.error, urllib.request

ACCEPT = "application/vnd.koreader.v1+json"
# Backhog never registers accounts over kosync: an existing name gets the
# reference's own "already registered" 402, a new one 403.
ALLOWED = {"create user": "registration is closed by design (402/403)"}


def call(base, method, path, body=None, user=None, key=None):
    headers = {"Accept": ACCEPT, "Content-Type": "application/json"}
    if user is not None:
        headers["x-auth-user"], headers["x-auth-key"] = user, key
    req = urllib.request.Request(base + path, method=method, headers=headers,
                                 data=json.dumps(body).encode() if body is not None else None)
    ctx = ssl._create_unverified_context()  # the reference image ships a self-signed cert
    try:
        with urllib.request.urlopen(req, context=ctx) as r:
            return r.status, r.read()
    except urllib.error.HTTPError as e:
        return e.code, e.read()


def shape(raw):
    try:
        v = json.loads(raw or b"null")
    except ValueError:
        return "<not json>"
    if isinstance(v, dict):
        return {k: type(x).__name__ for k, x in sorted(v.items())}
    return type(v).__name__


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--ref", required=True)
    ap.add_argument("--backhog", required=True)
    ap.add_argument("--user", required=True)
    ap.add_argument("--token", required=True)
    ap.add_argument("--document", required=True)
    a = ap.parse_args()

    md5 = lambda s: hashlib.md5(s.encode()).hexdigest()
    servers = {
        "ref": (a.ref, "kosync-diff-user", md5("kosync-diff-password")),
        "backhog": (a.backhog, a.user, md5(a.token)),
    }
    doc = a.document.lower()
    push = {"document": doc, "progress": "/body/DocFragment[2]/body/p[1]/text().0",
            "percentage": 0.25, "device": "kosync-diff", "device_id": "KOSYNCDIFF"}
    rich = dict(push, percentage=0.5,
                metadata={"filename": "x.epub", "title": "X", "authors": "Y"},
                position={"pctQ": 5000, "spine": 1, "page": 3, "pages": 9, "xpath": push["progress"]})
    steps = [
        ("create user", lambda s: call(s[0], "POST", "/users/create", {"username": s[1], "password": s[2]})),
        ("auth ok", lambda s: call(s[0], "GET", "/users/auth", user=s[1], key=s[2])),
        ("auth bad key", lambda s: call(s[0], "GET", "/users/auth", user=s[1], key=md5("wrong"))),
        ("auth no headers", lambda s: call(s[0], "GET", "/users/auth")),
        ("pull unknown doc", lambda s: call(s[0], "GET", "/syncs/progress/" + "0" * 32, user=s[1], key=s[2])),
        ("push", lambda s: call(s[0], "PUT", "/syncs/progress", push, s[1], s[2])),
        ("pull after push", lambda s: call(s[0], "GET", "/syncs/progress/" + doc, user=s[1], key=s[2])),
        ("push with CrossPoint extras", lambda s: call(s[0], "PUT", "/syncs/progress", rich, s[1], s[2])),
        ("push unauthenticated", lambda s: call(s[0], "PUT", "/syncs/progress", push)),
    ]
    failures = 0
    for name, step in steps:
        (rs, rb), (bs, bb) = step(servers["ref"]), step(servers["backhog"])
        same = rs == bs and shape(rb) == shape(bb)
        verdict = "ok" if same else ("allowed: " + ALLOWED[name] if name in ALLOWED else "DIFF")
        failures += verdict == "DIFF"
        print(f"{verdict:>8}  {name}: ref {rs} {shape(rb)} | backhog {bs} {shape(bb)}")
    sys.exit(1 if failures else 0)


if __name__ == "__main__":
    main()
