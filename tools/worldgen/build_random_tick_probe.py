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
    traps = []
    fires = []
    fire_odds = {}
    copper = []
    hits = []
    conversions = []
    creepers = []
    piglin = None
    living = {}
    copper_stages = {}
    horse = None
    skeleton = None
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
        elif line.startswith(b'trap '):
            traps.append(json.loads(line[5:]))
        elif line.startswith(b'fire '):
            fires.append(json.loads(line[5:]))
        elif line.startswith(b'fireOdds '):
            fire_odds = json.loads(line[8:])
        elif line.startswith(b'copper '):
            copper.append(json.loads(line[7:]))
        elif line.startswith(b'hit '):
            hits.append(json.loads(line[4:]))
        elif line.startswith(b'conversion '):
            conversions.append(json.loads(line[11:]))
        elif line.startswith(b'creeper '):
            creepers.append(json.loads(line[8:]))
        elif line.startswith(b'piglin '):
            piglin = bytes.fromhex(line[7:].decode())
        elif line.startswith(b'living '):
            living = json.loads(line[7:])
        elif line.startswith(b'copperStages '):
            copper_stages = json.loads(line[12:])
        elif line.startswith(b'horse '):
            horse = bytes.fromhex(line[6:].decode())
        elif line.startswith(b'skeleton '):
            skeleton = bytes.fromhex(line[9:].decode())
        elif line.startswith(b'sizes '):
            sizes = json.loads(line[6:])
        elif line.startswith(b'flags '):
            flags = bytes(json.loads(line[6:]))
    if len(lightning) != 2 or len(sizes) != 130 or len(precipitation) != 13 or len(cases) != 26 or flags is None or len(flags) != 26684:
        raise RuntimeError('Incomplete original random tick capture')
    # One tick per line keeps individual state changes inspectable in diffs.
    def dump(value):
        return json.dumps(value, separators=(',', ':'))

    def trajectories(scenes, start='[', end=']'):
        lines = [start]
        for index, scene in enumerate(scenes):
            header = {key: value for key, value in scene.items() if key != 'ticks'}
            lines.append(dump(header)[:-1] + ',"ticks":[')
            lines.append(',\n'.join(map(dump, scene['ticks'])))
            lines.append(']}' + (',' if index + 1 < len(scenes) else ''))
        return ('\n'.join([*lines, end]) + '\n').encode()
    payloads = {
        'testdata/worldgen/lightning_creepers_1.21.1.json': trajectories(creepers),
        'internal/world/zombified_piglin_1.21.1.bin': piglin,
        'testdata/worldgen/lightning_conversions_1.21.1.json': trajectories(conversions),
        'internal/world/living_damage_1.21.1.json': (json.dumps(living, sort_keys=True, separators=(',', ':')) + '\n').encode(),
        'testdata/worldgen/lightning_hits_1.21.1.json': trajectories(hits),
        'internal/world/copper_weathering_1.21.1.json': (json.dumps(copper_stages, indent=2) + '\n').encode(),
        'testdata/worldgen/lightning_copper_1.21.1.json': trajectories(copper),
        'internal/world/fire_odds_1.21.1.json': (json.dumps(fire_odds, separators=(',', ':')) + '\n').encode(),
        'testdata/worldgen/lightning_fire_1.21.1.json': trajectories(fires),
        'testdata/worldgen/skeleton_traps_1.21.1.json': ('[\n' + ',\n'.join(map(dump, traps)) + '\n]\n').encode(),
        'internal/world/skeleton_trap_1.21.1.bin': horse,
        'internal/world/skeleton_rider_1.21.1.bin': skeleton,
        'testdata/worldgen/precipitation_1.21.1.json': ('[\n' + ',\n'.join(map(dump, precipitation)) + '\n]\n').encode(),
        'testdata/worldgen/lightning_1.21.1.json': ('[\n' + ',\n'.join(map(dump, lightning)) + '\n]\n').encode(),
        'internal/world/entity_sizes_1.21.1.json': (json.dumps(sizes, indent=None, separators=(',', ':')) + '\n').encode(),
        'internal/world/random_tick_states_1.21.1.bin': flags,
        'testdata/worldgen/random_ticks_1.21.1.json': trajectories(cases, '{"version":"1.21.1","cases":[', ']}'),
    }
    if len(traps) != 11 or not horse or not skeleton or len(fires) != 17 or not fire_odds:
        raise RuntimeError('Incomplete original skeleton trap capture')
    if len(conversions) != 21 or not piglin:
        raise RuntimeError('Incomplete original lightning conversion capture')
    if len(creepers) != 6 or any(len(scene['ticks']) != 15 for scene in creepers):
        raise RuntimeError('Incomplete original charged creeper capture')
    if len(copper) != 16 or len(copper_stages) != 36:
        raise RuntimeError('Incomplete original copper capture')
    if len(hits) != 8 or 'minecraft:cow' not in living['mobs'] or 'minecraft:iron_helmet' not in living['equipment']:
        raise RuntimeError('Incomplete original lightning hit capture')
    for name, data in payloads.items():
        path = common.ROOT / name
        if args.check:
            existing = (path.read_text(encoding='utf-8').encode() if path.suffix == '.json'
                        else path.read_bytes())
            if existing != data:
                raise RuntimeError(f'Original random tick reference changed: {name}')
        else:
            common.atomic_write(path, data)
    print(f'{"Checked" if args.check else "Published"} {len(cases)} random-tick cases, {len(precipitation)} precipitation cases, {len(lightning)} lightning cases, {len(traps)} skeleton trap cases, {len(fires)} fire cases and {len(flags)} state flags')
    print(f'{len(copper)} lightning copper cases and {len(copper_stages)} weathering blocks')
    print(f'{len(conversions)} lightning conversion cases')
    print(f'{len(creepers)} charged creeper cases')
    print(f'{len(hits)} living damage cases, {len(living["mobs"])} living types and {len(living["equipment"])} armor items')


if __name__ == '__main__':
    main()
