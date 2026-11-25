"""Capture section sampling, leaf decay, farmland and crop trajectories from Minecraft Java 1.21.1."""
import argparse
import json
import subprocess

from _bootstrap import common


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true', help='compare the capture with committed files')
    args = parser.parse_args()
    directory = common.ROOT / 'artifacts/random-tick-reference'
    classes = directory / 'classes'
    java, javac = common.java_tools()
    classpath = common.reference_classpath(classes)
    probes = common.ROOT / 'tools/worldgen/probes'
    names = ['RandomTickLevel.java', 'RandomTickProbe.java', 'RuinedPortalProbeSupport.java', 'IglooProbeSupport.java']
    subprocess.run([javac, '-proc:none', '-cp', classpath, '-d', classes,
                    *(probes / name for name in names)], check=True)
    capture = subprocess.run([java, '-Xmx512m', '-cp', classpath, 'atlasprobe.RandomTickProbe'],
                             cwd=directory, stdout=subprocess.PIPE, check=True)
    cases = []
    lightning = []
    precipitation = []
    sizes = {}
    flags = None
    for line in capture.stdout.splitlines():
        if line.startswith(b'case '):
            cases.append(json.loads(line[5:]))
        elif line.startswith(b'precipitation '):
            precipitation.append(json.loads(line[14:]))
        elif line.startswith(b'lightning '):
            lightning.append(json.loads(line[10:]))
        elif line.startswith(b'sizes '):
            sizes = json.loads(line[6:])
        elif line.startswith(b'flags '):
            flags = bytes(json.loads(line[6:]))
    if len(lightning) != 2 or len(sizes) != 130 or len(precipitation) != 13 or len(cases) != 26 or flags is None or len(flags) != 26684:
        raise RuntimeError('Incomplete original random tick capture')
    # One tick per line keeps individual state changes inspectable in diffs.
    lines = ['{"version":"1.21.1","cases":[']

    def dump(value):
        return json.dumps(value, separators=(',', ':'))

    for index, case in enumerate(cases):
        ticks = case.pop('ticks')
        lines.append(dump(case)[:-1] + ',"ticks":[')
        lines.extend(dump(tick) + (',' if i + 1 < len(ticks) else '') for i, tick in enumerate(ticks))
        lines.append(']}' + (',' if index + 1 < len(cases) else ''))
    lines.append(']}')
    payloads = {
        'testdata/worldgen/precipitation_1.21.1.json': ('[\n' + ',\n'.join(map(dump, precipitation)) + '\n]\n').encode(),
        'testdata/worldgen/lightning_1.21.1.json': ('[\n' + ',\n'.join(map(dump, lightning)) + '\n]\n').encode(),
        'internal/world/entity_sizes_1.21.1.json': (json.dumps(sizes, indent=None, separators=(',', ':')) + '\n').encode(),
        'internal/world/random_tick_states_1.21.1.bin': flags,
        'testdata/worldgen/random_ticks_1.21.1.json': ('\n'.join(lines) + '\n').encode(),
    }
    for name, data in payloads.items():
        path = common.ROOT / name
        if args.check:
            existing = (path.read_text(encoding='utf-8').encode() if path.suffix == '.json'
                        else path.read_bytes())
            if existing != data:
                raise RuntimeError(f'Original random tick reference changed: {name}')
        else:
            common.atomic_write(path, data)
    print(f'{"Checked" if args.check else "Published"} {len(cases)} random-tick cases, {len(precipitation)} precipitation cases, {len(lightning)} lightning cases and {len(flags)} state flags')


if __name__ == '__main__':
    main()
