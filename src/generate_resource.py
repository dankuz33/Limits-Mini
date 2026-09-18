#!/usr/bin/env python3
"""Generate an x64 COFF .rsrc object with an application icon and asInvoker manifest.
Uses only the Python standard library. End users do not need Python.
The generated object is included so rebuilding the app requires only Go.
"""
from pathlib import Path
import struct

ROOT = Path(__file__).resolve().parent
icon = (ROOT / 'assets' / 'app.ico').read_bytes()
reserved, kind, count = struct.unpack_from('<HHH', icon)
assert reserved == 0 and kind == 1 and count > 0
leaves = {}
group = bytearray(struct.pack('<HHH', 0, 1, count))
for i in range(count):
    w, h, colors, res, planes, bpp, size, offset = struct.unpack_from('<BBBBHHII', icon, 6+i*16)
    leaves[(3, i+1, 1033)] = icon[offset:offset+size]
    group.extend(struct.pack('<BBBBHHIH', w,h,colors,res,planes,bpp,size,i+1))
leaves[(14, 1, 1033)] = bytes(group)
manifest = b'''<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
 <assemblyIdentity version="0.1.0.0" processorArchitecture="amd64" name="LimitsMini" type="win32"/>
 <description>Limits Mini: Claude Code and Codex usage</description>
 <dependency><dependentAssembly><assemblyIdentity type="win32" name="Microsoft.Windows.Common-Controls" version="6.0.0.0" processorArchitecture="*" publicKeyToken="6595b64144ccf1df" language="*"/></dependentAssembly></dependency>
 <trustInfo xmlns="urn:schemas-microsoft-com:asm.v3"><security><requestedPrivileges>
  <requestedExecutionLevel level="asInvoker" uiAccess="false"/>
 </requestedPrivileges></security></trustInfo>
 <compatibility xmlns="urn:schemas-microsoft-com:compatibility.v1"><application>
  <supportedOS Id="{8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a}"/>
 </application></compatibility>
 <application xmlns="urn:schemas-microsoft-com:asm.v3"><windowsSettings>
  <dpiAware xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true/pm</dpiAware>
  <dpiAwareness xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">PerMonitorV2</dpiAwareness>
 </windowsSettings></application>
</assembly>
'''
leaves[(24, 1, 1033)] = manifest
# Allocate the three resource directory levels before their data entries.
tree = {}
for (typ, name, lang), data in leaves.items():
    tree.setdefault(typ, {}).setdefault(name, {})[lang] = data
buf = bytearray()
relocations = []
def aligned():
    while len(buf) % 4: buf.append(0)
def directory(entries):
    off = len(buf)
    buf.extend(struct.pack('<IIHHHH',0,0,0,0,0,len(entries)))
    buf.extend(b'\0'*(8*len(entries)))
    for i, key in enumerate(sorted(entries)):
        value = entries[key]
        if isinstance(value,dict):
            target = directory(value) | 0x80000000
        else:
            aligned()
            target = len(buf)
            buf.extend(b'\0'*16)
            aligned()
            dataoff = len(buf)
            buf.extend(value)
            aligned()
            struct.pack_into('<IIII', buf, target, dataoff, len(value), 0, 0)
            relocations.append(target)
        struct.pack_into('<II',buf,off+16+i*8,key,target)
    return off
directory(tree)
raw = bytes(buf)
raw_pointer = 20+40
reloc_pointer = raw_pointer+len(raw)
sym_pointer = reloc_pointer+len(relocations)*10
header = struct.pack('<HHIIIHH',0x8664,1,0,sym_pointer,1,0,0)
section = struct.pack('<8sIIIIIIHHI',b'.rsrc\0\0\0',0,0,len(raw),raw_pointer,reloc_pointer,0,len(relocations),0,0x40000040)
relocs = b''.join(struct.pack('<IIH',off,0,0x0003) for off in relocations)
symbol = struct.pack('<8sIhHBB',b'.rsrc\0\0\0',0,1,0,3,0)
(ROOT/'resource_windows_amd64.syso').write_bytes(header+section+raw+relocs+symbol+struct.pack('<I',4))
print('Resource object generated:',len(raw),'bytes;',len(relocations),'leaves')
