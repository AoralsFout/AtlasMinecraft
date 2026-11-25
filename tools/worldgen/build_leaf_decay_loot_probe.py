"""Capture consecutive empty-tool leaf drops from Java Edition 1.21.1."""
import argparse
import json
import subprocess
import zipfile
from _bootstrap import common


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args()
    target = common.ROOT / 'artifacts/leaf-decay-loot-probe'
    classes = target / 'classes'
    java, javac = common.java_tools()
    classpath = common.reference_classpath(classes)
    directory = common.ROOT / 'tools/worldgen/probes'
    sources = ['IglooProbeSupport.java', 'RuinedPortalProbeSupport.java', 'LeafDecayLootProbe.java']
    subprocess.run([javac, '-proc:none', '-cp', classpath, '-d', str(classes),
                    *(str(directory / source) for source in sources)], cwd=target, check=True)
    with zipfile.ZipFile(common.reference_server()) as jar:
        leaves = sorted(path.split('/')[-1][:-5] for path in jar.namelist()
                        if path.startswith('data/minecraft/loot_table/blocks/') and path.endswith('_leaves.json'))
    if len(leaves) != 10:
        raise ValueError(f'Expected 10 leaf tables, found {leaves}')
    seed, rolls = 1234, 512
    result = target / 'tables.json'
    subprocess.run([java, '-Xmx512m', '-cp', classpath, 'atlasprobe.LeafDecayLootProbe',
                    str(result), str(seed), str(rolls), *leaves], cwd=target, check=True)
    tables = json.loads(result.read_bytes())
    # One line per nonempty roll keeps seed-specific changes visible in diffs.
    rows = [f'{{"seed": {seed}, "rolls": {rolls}, "tables": {{']
    for index, (leaf, events) in enumerate(tables.items()):
        rows.append(f'  "{leaf}": {{')
        rows.append(',\n'.join(f'    "{roll}": {json.dumps(stacks)}' for roll, stacks in events.items()))
        rows.append('  }' + (',' if index + 1 < len(tables) else ''))
    encoded = ('\n'.join([*rows, '}}']) + '\n').encode()
    fixture = common.ROOT / 'testdata/worldgen/leaf_decay_loot_1.21.1.json'
    if args.check:
        if fixture.read_text(encoding='utf-8').encode() != encoded:
            raise RuntimeError(f'Original leaf loot differs from {fixture}')
    else:
        common.atomic_write(fixture, encoded)
    print(f'{"Checked" if args.check else "Captured"} {rolls * len(tables)} original leaf loot rolls')


if __name__ == '__main__':
    main()
