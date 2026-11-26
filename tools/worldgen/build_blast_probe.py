"""Run fixed-seed mob explosions on the pinned 1.21.1 dedicated server."""
import argparse
import gzip
import json
from pathlib import Path
import subprocess
import tempfile
import zipfile

from _bootstrap import common


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args()
    base = common.ROOT / 'artifacts/blast-reference'
    classes = base / 'classes'
    java, javac = common.java_tools()
    cp = common.reference_classpath(classes)
    export = '--add-exports=java.base/jdk.internal.org.objectweb.asm=ALL-UNNAMED'
    sources = common.ROOT / 'tools/worldgen/probes'
    subprocess.run([javac, export, '-proc:none', '-cp', cp, '-d', classes,
                    sources / 'BlastProbe.java', sources / 'BlastProbeAgent.java'], check=True)
    agent = base / 'probe.jar'
    with zipfile.ZipFile(agent, 'w') as archive:
        archive.writestr('META-INF/MANIFEST.MF', 'Manifest-Version: 1.0\nPremain-Class: atlasprobe.BlastProbeAgent\n\n')
        for path in classes.rglob('BlastProbeAgent*.class'):
            archive.write(path, path.relative_to(classes))
    with tempfile.TemporaryDirectory(dir=base) as tmp:
        run = Path(tmp)
        (run / 'eula.txt').write_text('eula=true\n')
        (run / 'server.properties').write_text('server-ip=127.0.0.1\nserver-port=0\nonline-mode=false\n'
            'level-seed=1234\nlevel-type=minecraft:flat\ngenerate-structures=false\nview-distance=2\n')
        subprocess.run([java, export, '-XX:ActiveProcessorCount=3', '-Xmx768m', '-javaagent:'+str(agent),
                        '-cp', cp, 'net.minecraft.server.Main', 'nogui'], cwd=run, timeout=180, check=True)
        with zipfile.ZipFile(common.reference_server()) as jar:
            prefix = 'data/minecraft/loot_table/blocks/'
            tables = {name[len(prefix):-5]: json.loads(jar.read(name)) for name in jar.namelist()
                      if name.startswith(prefix) and name.endswith('.json')}
        assets = dict(tables=tables, aliases=json.loads((run/'aliases.json').read_text()),
                      resistance=json.loads((run/'resistance.json').read_text()))
        (run/'blocks.json').write_text(json.dumps(assets))
        for src, dest in [('blast.json','testdata/worldgen/creeper_blast_1.21.1.json'),
                          ('blocks.json','internal/world/blast_blocks_1.21.1.json.gz')]:
            path = common.ROOT / dest
            result = json.dumps(json.loads((run/src).read_text()), sort_keys=True, separators=(',', ':')).encode() + b'\n'
            if dest.endswith('.gz'):
                result = gzip.compress(result, mtime=0)
            if args.check:
                actual = path.read_bytes()
                if (gzip.decompress(actual) != gzip.decompress(result) if dest.endswith('.gz') else actual != result):
                    raise SystemExit(f'original capture differs: {path}')
            else:
                common.atomic_write(path, result)


if __name__ == '__main__':
    main()
