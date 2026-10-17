#!/usr/bin/env python3
"""ABI smoke test: loads the built dynamic library the way the host does.

Go forbids cgo inside _test.go files, so the dynamic library contract is
exercised here instead: init the function table, then drive the call entry
point with one success and one error method and check the JSON envelopes.
"""
import ctypes
import json
import sys


class Buffer(ctypes.Structure):
    _fields_ = [("ptr", ctypes.c_void_p), ("len", ctypes.c_size_t)]


HostCall = ctypes.CFUNCTYPE(
    ctypes.c_int, ctypes.c_void_p, ctypes.c_char_p,
    ctypes.POINTER(ctypes.c_uint8), ctypes.c_size_t, ctypes.POINTER(Buffer),
)
HostFree = ctypes.CFUNCTYPE(None, ctypes.c_void_p, ctypes.c_size_t)
PluginCall = ctypes.CFUNCTYPE(
    ctypes.c_int, ctypes.c_char_p,
    ctypes.POINTER(ctypes.c_uint8), ctypes.c_size_t, ctypes.POINTER(Buffer),
)
PluginFree = ctypes.CFUNCTYPE(None, ctypes.c_void_p, ctypes.c_size_t)
PluginShutdown = ctypes.CFUNCTYPE(None)


class HostAPI(ctypes.Structure):
    _fields_ = [
        ("abi_version", ctypes.c_uint32),
        ("host_ctx", ctypes.c_void_p),
        ("call", HostCall),
        ("free_buffer", HostFree),
    ]


class PluginAPI(ctypes.Structure):
    _fields_ = [
        ("abi_version", ctypes.c_uint32),
        ("call", PluginCall),
        ("free_buffer", PluginFree),
        ("shutdown", PluginShutdown),
    ]


def fail(message):
    print(f"FAIL: {message}")
    sys.exit(1)


def main(path):
    lib = ctypes.CDLL(path)
    lib.cliproxy_plugin_init.restype = ctypes.c_int
    lib.cliproxy_plugin_init.argtypes = [ctypes.POINTER(HostAPI), ctypes.POINTER(PluginAPI)]

    api = PluginAPI()
    if lib.cliproxy_plugin_init(None, ctypes.byref(api)) != 0:
        fail("cliproxy_plugin_init returned non-zero")
    if api.abi_version != 1:
        fail(f"abi_version = {api.abi_version}, want 1")
    for name in ("call", "free_buffer", "shutdown"):
        if not getattr(api, name):
            fail(f"{name} function pointer is nil")

    if lib.cliproxy_plugin_init(None, None) == 0:
        fail("cliproxy_plugin_init accepted a nil plugin table")

    def invoke(method, payload=b""):
        buf = Buffer()
        request = None
        if payload:
            request = (ctypes.c_uint8 * len(payload)).from_buffer_copy(payload)
        api.call(method.encode(), request, len(payload), ctypes.byref(buf))
        if not buf.ptr:
            fail(f"{method} returned an empty response buffer")
        raw = ctypes.string_at(buf.ptr, buf.len)
        api.free_buffer(buf.ptr, buf.len)
        return json.loads(raw)

    ok = invoke("plugin.quiesce")
    if not ok.get("ok"):
        fail(f"plugin.quiesce envelope = {ok}, want ok=true")

    err = invoke("model.static")
    if err.get("ok"):
        fail(f"model.static before register = {err}, want an error envelope")
    if err.get("error", {}).get("code") != "not_registered":
        fail(f"model.static error code = {err.get('error')}, want not_registered")

    print(f"ABI smoke passed: {path}")


if __name__ == "__main__":
    main(sys.argv[1] if len(sys.argv) > 1 else "dist/deepseek-cpa.linux.amd64")
