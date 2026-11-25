# Java 1.21.1 树叶衰减与掉落

## 移除原木后的方块变化

树叶收到相邻方块的形状更新时，若含水，先按水的延迟登记流体刻。除“新邻居属于 `#minecraft:logs` 且当前距离已经是 1”外，树叶均登记延迟 1 刻的方块刻。方块刻重新读取六个面邻居。原木的距离为 0，带树叶距离属性的方块提供当前距离，其他方块按 7 计算。新距离取六邻居最小距离加一，上限 7。写入使用标志 3，保留 `persistent` 和 `waterlogged`。相邻树叶因而可以在后续方块刻继续传播距离。[原版服务端映射，LeavesBlock.updateShape、tick、getOptionalDistanceAt](https://piston-data.mojang.com/v1/objects/03f8985492bda0afc0898465341eb0acef35f570/server.txt)

仅 `distance=7,persistent=false` 的树叶具备随机刻资格。命中时，`LeavesBlock.randomTick` 重新检查这两个属性，先调用 `Block.dropResources`，再调用 `Level.removeBlock(pos,false)`。干树叶替换为空气，含水树叶替换为水源，两者都使用标志 3。衰减条件本身没有额外概率抽取。[官方 Java 1.21.1 服务端及校验值](https://piston-meta.mojang.com/v1/packages/c086fab6229047df03bb78951c7a1b8fce8c4892/1.21.1.json)

## 空工具掉落池

下表列出原版服务端包 `data/minecraft/loot_table/blocks/*_leaves.json` 中自然衰减能命中的所有物品。每行从左到右依次结算，允许一次衰减掉落多个物品。每个有概率的池取一次 `nextFloat() < chance`。木棍命中后立即取 `1 + nextInt(2)` 决定数量。[官方服务端包](https://piston-data.mojang.com/v1/objects/59353fb40c36d304f2035d51e7d6e6baa98dc05c/server.jar)

| 树叶 | 幼苗或灌木，1 个 | 木棍，1–2 根 | 苹果，1 个 |
| --- | --- | --- | --- |
| 橡树、深色橡树 | 对应树苗 5% | 2% | 0.5% |
| 云杉、白桦、金合欢、樱花 | 对应树苗 5% | 2% | — |
| 丛林 | 丛林树苗 2.5% | 2% | — |
| 杜鹃、盛开的杜鹃 | 对应杜鹃灌木 5% | 2% | — |
| 红树 | — | 2% | — |

每种树叶的掉落表指定独立的命名随机序列，例如 `minecraft:blocks/oak_leaves`。默认以世界种子生成未混合的两个 64 位字，与序列名 UTF-8 的 MD5 两个大端 64 位字逐一异或，再各自做 Stafford13 混合，初始化 Xoroshiro128++。主世界保存这组序列，其他维度共享同一实例。存档 `data/random_sequences.dat` 的 `data.sequences.<完整序列名>.source` 是含低、高状态字的 NBT LongArray。`data.salt` 默认为 0，`include_world_seed` 和 `include_sequence_id` 默认均为真。自定义配置先将选用的世界种子（或 0）与符号扩展至 64 位的 salt 异或，再按开关决定是否混入序列名。[原版服务端映射，RandomSequences、RandomSequence、RandomSupport、MinecraftServer.createLevels](https://piston-data.mojang.com/v1/objects/03f8985492bda0afc0898465341eb0acef35f570/server.txt)

掉落实体的随机数取自关卡。每个命中的物品堆先按 X、Y、Z 的顺序取三次 `nextDouble()` 确定散落位置，即使 `doTileDrops=false` 也会推进。允许生成实体时再取两次决定 X、Z 水平速度，垂直初速为 0.2，初始拾取延迟为 10。之后被选中的小麦可能受这些关卡随机数调用影响。[原版服务端映射，Block.popResource、ItemEntity](https://piston-data.mojang.com/v1/objects/03f8985492bda0afc0898465341eb0acef35f570/server.txt)

## 原版复查

在仓库根目录使用 JDK 21 和官方 `jar/server.jar` 运行以下命令。探针通过原版 codec 解码十份掉落表，向原版 `LootTable.getRandomItemsRaw` 传入空工具和该表的 `RandomSequences.get` 随机源。固定世界种子 1234，每种树叶连续结算 512 次。向量记录非空结果的零基索引和有序物品堆，其余索引为空。5,120 次结算中 347 次产生物品，共 357 堆。这项比较覆盖掉落表和序列推进，区块随机抽样与实体物理由各自的世界刻检查覆盖。[探针](../../tools/worldgen/probes/LeafDecayLootProbe.java) · [原版向量](../../testdata/worldgen/leaf_decay_loot_1.21.1.json)

```sh
python tools/worldgen/build_leaf_decay_loot_probe.py --check
```
