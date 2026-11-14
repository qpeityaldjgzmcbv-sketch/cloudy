"""Copy Windows resources as data from a matching original Cloudy executable.

Keeps Cloudy's existing icon, manifest and application identity. Does not run
or modify the source executable and does not create an installer.
"""
from pathlib import Path
import argparse
import struct


def coff_resources(source):
    image=Path(source).read_bytes()
    if image[:2]!=b'MZ': raise ValueError('Expected a Windows executable')
    pe=struct.unpack_from('<I',image,0x3c)[0]
    if image[pe:pe+4]!=b'PE\0\0': raise ValueError('Invalid PE header')
    machine,sections=struct.unpack_from('<HH',image,pe+4)
    if machine!=0x8664: raise ValueError('Expected an amd64 executable')
    optsize=struct.unpack_from('<H',image,pe+20)[0]
    section_table=pe+24+optsize
    for i in range(sections):
        off=section_table+40*i
        if image[off:off+8].rstrip(b'\0')==b'.rsrc':
            _,rva,size,fileoff=struct.unpack_from('<IIII',image,off+8)
            raw=bytearray(image[fileoff:fileoff+size]);break
    else: raise ValueError('Executable has no Windows resources')
    relocs=[]
    visited=set()
    def directory(offset):
        if offset in visited: raise ValueError('Cyclic resource directory')
        visited.add(offset)
        named,ids=struct.unpack_from('<HH',raw,offset+12)
        for j in range(named+ids):
            _,pointer=struct.unpack_from('<II',raw,offset+16+8*j)
            if pointer&0x80000000: directory(pointer&0x7fffffff)
            else:
                address,length=struct.unpack_from('<II',raw,pointer)
                local=address-rva
                if local<0 or local+length>len(raw): raise ValueError('Resource outside section')
                struct.pack_into('<I',raw,pointer,local)
                relocs.append(pointer)
    directory(0)
    rawat=60; relocat=rawat+len(raw); symbolsat=relocat+10*len(relocs)
    header=struct.pack('<HHIIIHH',machine,1,0,symbolsat,1,0,0)
    section=struct.pack('<8sIIIIIIHHI',b'.rsrc',0,0,len(raw),rawat,relocat,0,len(relocs),0,0x40000040)
    relocations=b''.join(struct.pack('<IIH',address,0,3) for address in relocs)
    symbol=struct.pack('<8sIhHBB',b'.rsrc',0,1,0,3,0)
    return header+section+raw+relocations+symbol+struct.pack('<I',4)

if __name__=='__main__':
    parser=argparse.ArgumentParser()
    parser.add_argument('source');parser.add_argument('output')
    args=parser.parse_args()
    target=Path(args.output)
    if target.exists(): raise SystemExit('Refusing to overwrite an existing resource file')
    target.write_bytes(coff_resources(args.source))
