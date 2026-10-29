"""Environment for Go's profile/trace parser subprocess, not the GUI process.

The native collector intentionally supplies a minimal environment. Go's tool
dispatcher still needs writable cache and temporary directories on Windows.
Do not fill in user-profile defaults or change the measured GOMAXPROCS layer.
"""

from pathlib import Path


def parser_env(diagnostic_env, cache_dir):
    """Return a copy with absolute writable cache/temp locations for Go tools.

    The caller owns the cache's parent (normally an attempt's parsing directory).
    Errors propagate before launching the parser; the diagnostic environment and
    profile bytes are never modified. An existing non-directory is an error.
    """
    cache = Path(cache_dir)
    if not cache.is_absolute():
        raise ValueError("Go tool cache must be an absolute path")
    temp = cache.parent / "go-tool-tmp"
    cache.mkdir(parents=True, exist_ok=True)
    temp.mkdir(parents=True, exist_ok=True)
    result = dict(diagnostic_env)
    result["GOCACHE"] = str(cache)
    result["GOTMPDIR"] = str(temp)
    return result
