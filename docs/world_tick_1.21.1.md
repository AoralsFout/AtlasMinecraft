# Java 1.21.1 游戏刻世界模拟调查

调查基线为 Atlas `cee446d`（2026-11-25）和 Java 版 1.21.1（DataVersion 3955）。当前服务器已经能按玩家票据推进计划方块刻、计划流体刻和部分下落实体。最先值得修的是两个玩家可见的行为：石按钮会在世界时间静止时松开，砍掉唯一原木后，树叶可能一直保留旧的叶距。两者都可以利用已有计划刻队列修复。[服务器入口](../internal/server/world_tick.go) · [世界入口](../internal/world/tick.go) · [原版发布页](https://www.minecraft.net/en-us/article/minecraft-java-edition-1-21-1)

## 首批实现进展

以下调查表和注册名统计保留在 `cee446d` 基线上。其后的三个本地提交已接通以下路径。

| 提交 | 已验证的行为 |
| --- | --- |
| `0aa6bb0` | 玩家挖掉唯一原木，跨区块的树叶在下一刻从距离 1 变为 7。六面树叶按原版形状更新顺序入队。 |
| `000216e` | 石按钮和磨制黑石按钮把 20 刻的松开请求与按下状态写入同一区块存档。重复点击不会延长脉冲，重启后按剩余刻数松开。计划方块刻现有 42 种非空分派，余 98 种。木质按钮、完整供电传播和按钮声效仍待实现。 |
| `a35bb31` | 世界刻保留同维度玩家的精确 X/Z 和旁观标记，并按区块就绪状态及严格小于 128 格的距离判定随机刻资格。当前联机玩家仍以创造模式进入。区段随机抽样、树叶凋落和作物生长留待后续接入。 |

## 区段随机刻、树叶凋落与小麦生长

玩家驱动的世界刻现已在计划流体刻之后、实体刻之前运行区段随机抽样。默认每个有资格的区段每刻抽三次，使用存档的 `randomTickSpeed`。区段资格来自 1.21.1 全部 26,684 个状态的原版标记。实际方块回调已接入十种树叶、耕地和小麦。耕地在被抽中时按附近的水、上方是否有雨和作物标签更新。顶部实心遮挡通过延迟一刻的计划更新转为泥土。其他作物、熔岩起火以及天气产生的冰雪仍待实现。[世界刻入口](../internal/world/tick.go) · [抽样与小麦实现](../internal/world/random_tick.go)

晴天与雨天的原版回放逐刻比较方块状态、区段和降水抽样坐标、坐标生成器和关卡随机源的结束状态。树叶另以原版掉落表连续结算 5,120 次，Go 回放在中途写出并重新读取命名随机序列。联机刻入口测试从移除含水树叶旁的原木开始，验证旁观者、速度为零、凋落后留下水源，以及重开存档后的掉落实体。[逐刻场景与复查方法](worldgen/random_ticks_1.21.1.md) · [树叶掉落与存档](worldgen/leaf_decay_1.21.1.md)

耕地新增 16 个原版场景，覆盖跨区块灌溉、雨雪差异、覆盖与撤除，以及相同随机条件下灌溉对小麦的影响。存档测试通过玩家放置入口覆盖耕地，重开后按剩余 1 刻执行。实体踩踏与耕地变成完整泥土时的实体抬升留在实体碰撞阶段。[耕地实现](../internal/world/farmland.go) · [存档测试](../internal/world/farmland_test.go)

## 无玩家时的强加载区块

`/forceload add|remove <x z> [x z]` 接受方块坐标，两个端点都计入范围，每次最多操作 256 个区块。`query [x z]` 和 `remove all` 作用于当前维度，也可以通过 `/execute in <dimension> run forceload ...` 操作其他维度。命令成功后票据已存入该维度的 `data/chunks.dat`，区块由后台加载。[命令入口](../internal/server/forceload.go) · [存档读写](../internal/world/forced_chunks.go)

一个强加载中心提供等级 31 的票据。中心可运行实体刻；中心及周围一圈可运行计划方块刻和流体刻；再外一圈提供已加载的邻区。多个来源覆盖同一区块时取最强等级，移除其中一个来源会保留其他来源的资格。随机刻另查是否有非旁观玩家在区块中心 128 格以内，边界取严格小于。[票据合并与阶段资格](../internal/world/simulation.go)

Java 1.21.1 专用服务端对照使用无玩家平坦世界，种子 1234，`spawnChunkRadius=0`。冻结后执行 `forceload add -1600 1600`，得到区块 `[-100,100]`。在有边墙、露天的石底上设置水和灌溉的小麦，把 `randomTickSpeed` 调到 4096，步进 20 刻：水抵达相邻格，小麦仍是年龄 0。保存重启后可查询和移除该票据。仓库保留原版写出的 NBT 样本，Go 测试直接打开它并检查更新后的坐标编码。[原版样本](../testdata/worldgen/forced_chunks_1.21.1.dat) · [存档兼容测试](../internal/world/forced_chunks_test.go)

Atlas 的服务器入口测试另检查玩家离开后的水流计时：5 刻的计划更新先走 2 刻，保存重启后再走 3 刻流出。移除最后一张票据后，新开的出水口保持干燥，空闲卸载可释放整个 5×5 邻域。[服务器回归测试](../internal/server/forceload_test.go)

下一个可独立验收的阶段是主世界出生区块票据。用 `spawnChunkRadius=0`、默认值 2 和迁移出生点三个原版场景核对中心、外圈及无玩家时的阶段资格。随后接入传送门临时票据及到期移除，再处理三维度共享时间与 `doDaylightCycle`。

## 基线中复现的两个差异

| 场景 | 本次在 Atlas 的实测结果 | 1.21.1 原版行为与修复入口 |
| --- | --- | --- |
| 在平坦世界通过玩家交互按下石按钮，随后停止推进世界，继续观察按钮 | 世界年龄始终为 0，`powered` 从 true 变成 false。`useBlock` 启动 Go 计时器，存档中没有这次松开的计划请求。 | `ButtonBlock.press` 安排同类方块的计划刻。石质按钮延迟 20 游戏刻，木质 30 刻；木质按钮到期还会检查箭实体。先接入石质与磨制黑石按钮，核对按下、松开时对按钮和支撑方块附近的通知，再接木质与箭。[玩家交互](../internal/server/block_interaction.go) · [ButtonBlock 方法映射](https://mappings.dev/1.21.1/net/minecraft/world/level/block/ButtonBlock.html) · [Wiki 按钮](https://minecraft.wiki/w/Button) |
| 在已加载的 3×3 区块中央放置一块原木与一块 `distance=1,persistent=false` 的橡树树叶，移除唯一原木，让中央区块执行 4 个游戏刻 | 叶距仍为 1。复现使用 `World.SetBlock`，与玩家破坏入口 `TryPlaceBlock` 汇入同一套邻接更新。现有叶距存盘测试手动入队，因此可以与这个差异同时成立。 | 邻接变化应安排延迟 1 刻的树叶更新。孤立树叶重算后距离为 7；随后的自然凋落另由随机刻触发。先让普通方块编辑调用已有的树叶形状回调，再用玩家砍树和跨区块叶链验收。[玩家破坏](../internal/server/play.go) · [编辑后的邻接更新](../internal/world/block_updates.go) · [树叶回调](../internal/worldgen/block_tick.go) · [现有测试](../internal/world/block_tick_runtime_test.go) |

两个场景均在临时平坦世界运行。按钮从玩家使用入口触发；树叶场景没有手工注入请求。原版树叶回调另有 14,280 个计划刻样本和包含移除原木的完整服务器场景。后者从原版初始队列开始回放，适合校验执行结果，还需用玩家操作验证请求如何进入队列。[原版树叶探针](../tools/worldgen/probes/BlockTickLevel.java) · [原版服务器场景](../tools/worldgen/build_fluid_runtime_fixture.py)

## 一个游戏刻执行什么

按原版 `ServerLevel.tick` 的调用顺序列出。表内阶段各有自己的运行条件。Atlas 的时钟以 50 ms 为目标间隔，一次唤醒最多追赶 5 个游戏刻，逐刻调用 `TickWorlds`，之后按本轮刻数另行处理玩家传送门。[原版 ServerLevel 方法映射](https://mappings.dev/1.21.1/net/minecraft/server/level/ServerLevel.html) · [Atlas 时钟](../cmd/atlas/main.go)

原版另由 `TickRateManager` 控制冻结、单步与速率。冻结期间仍可处理连接和区块管理。Atlas 的管理命令已有 TPS/MSPT 观测，尚缺相应的模拟控制。为后续联机验收增加单步时，应让一次步进精确跨过一个世界刻，同时允许客户端继续收发包。[Atlas 命令](../internal/server/commands.go) · [原版服务端映射](https://piston-data.mojang.com/v1/objects/03f8985492bda0afc0898465341eb0acef35f570/server.txt)

| 原版阶段 | Atlas 当前入口与已有行为 | 待实现功能及验收场景 |
| --- | --- | --- |
| 世界边界、天气、睡眠、天空亮度、时间 | 各维度的 `advanceTimeLocked` 同时增加世界年龄与昼夜时间。可设置时间和晴雨雷，存档读写天气布尔值。[时钟](../internal/world/world.go) · [存档](../internal/world/vanilla.go) · [时间与天气包](../internal/server/commands.go) | 原版由主世界推进共享时间、处理定时事件，`doDaylightCycle` 控制日时；派生维度读取主世界时间。补游戏规则和客户端时间同步、天气倒计时与强度、睡眠跳夜、边界变化。验收关闭昼夜循环后跨三维度推进并重启，年龄仍增加，日时保持，计划刻照常触发。 |
| 计划方块刻 → 计划流体刻 | 两套队列各有 65,536 的每刻上限，共享子刻序号；已有去重、过期类型检查、激活、存盘与卸载。在线路径把失败请求延后 20 刻重试，继续执行其他请求。[调度执行](../internal/world/fluid_runtime.go) · [队列所有者](../internal/world/scheduled_tick_owner.go) · [队列参考](worldgen/fluid_simulation_1.21.1.md) | 补上游邻居通知和下表 100 种非空处理；优先按钮、珊瑚失水，再做依赖电力传播的中继器、比较器、观察者。用同刻不同优先级、方块被替换、加载边界和重启验证。 |
| 袭击 → 区块阶段（含生成生物、区块天气和随机刻） | `TickPlayers` 汇总玩家区块并异步准备地形。当前 `tickLocked` 运行完计划更新即进入下落实体。地形生成阶段能放置动物和结构实体。[票据](../internal/world/simulation.go) · [运行时入口](../internal/world/tick.go) · [初始动物](worldgen/initial_animals_1.21.1.md) | 增加逐刻区块选择、区段抽样、冰雪降水与雷击。自然刷怪还需要玩家距离、类别上限、生成条件和消失条件，袭击另有状态。验收关闭 `doMobSpawning` 时农作物仍可收到随机刻。 |
| 方块事件 | 玩家编辑与流体编辑各有邻接更新；已有向客户端发送场景效果的路径。[普通编辑](../internal/world/block_updates.go) · [流体回调](../internal/world/fluid_runtime.go) | 补 `ServerLevel.runBlockEvents` 的排队、再检查和执行，例如活塞。用同刻多次触发、执行前替换方块、区块暂时失去资格验证。幽匿振动另需游戏事件监听与传播。[游戏事件回调现状](../internal/world/fluid_container_runtime.go) |
| 龙战、实体、方块实体 → 实体管理 | 支持沙、红沙、沙砾及两种可疑方块的下落运动，并能生成部分掉落物。跨区块下落实体显式返回错误。方块实体可保存和同步，营火能保存烹饪进度。[实体运动](../internal/worldgen/falling_entity.go) · [运行与迁移](../internal/world/falling_entities.go) · [方块实体同步](../internal/world/block_entities.go) | 先补区块/区域间实体交接和掉落物的移动、拾取、合并、消失；再接通用实体与方块实体 tick，例如熔炉、漏斗、营火。后续覆盖乘客、生物 AI、末影龙战。验收同一 UUID 越界后只更新一次，卸载重载后库存和冷却接续。 |

## 区块的执行资格

1. **计划刻**。原版两套 `LevelTicks` 都使用 `isPositionTickingWithEntitiesLoaded`，要求实体存储就绪、区块持有者存在、达到方块 ticking 等级且 ticking chunk 已就绪。这里没有 128 格玩家距离检查。Atlas 已区分玩家模拟半径、额外一圈计划更新和最外一圈仅加载区块，并按邻域就绪程度执行。仍缺出生区块、持久强加载、传送门等票据来源以及相应的升级、降级与到期。启动的出生点预加载只预备地形。[Atlas 票据](../internal/world/simulation.go) · [区块就绪检查](../internal/world/scheduled_tick_owner.go) · [Wiki 区块历史](https://minecraft.wiki/w/Chunk#History)
2. **随机刻**。1.21.1 的 `ServerChunkCache.tickChunks` 打乱候选区块顺序。除了 ticking 资格，还要求同维度有非旁观玩家，而且玩家到区块中心的水平距离平方严格小于 16,384。玩家的 Y 不参与计算，同一区块内移动也可能让远端区块跨过门槛。当前 `SimulationCenter` 只保留游玩状态、维度和区块坐标。需保留精确 X/Z、旁观状态，并区分加载票据与邻近玩家判定。验收覆盖恰好 128 格、略小于 128 格、旁观玩家、多人重叠、只有强加载票据等情况。1.21.5 才放宽远离玩家时的随机刻规则。[玩家快照](../internal/player/dimension.go) · [Wiki 游戏刻历史](https://minecraft.wiki/w/Tick#History) · [Mojang 版本变更说明](https://feedback.minecraft.net/hc/en-us/articles/34065446883725-Minecraft-Java-Edition-Snapshot-25w06a)
3. **实体与方块实体**。原版在维度没有玩家且强加载集合为空时，前 300 个空刻仍可进入实体和方块实体阶段，此后跳过这一段，世界其他阶段仍按自己的条件运行。阶段内还要检查每个对象的区块资格；300 刻宽限本身不会保活区块。方块实体另查世界边界、区块状态和方块类型。Atlas 在最后一名玩家离开后清除模拟票据，该维度随即只推进时间。验收应区分无人但有强加载、只有出生区块票据、完全不满足区块资格三种情况。[Atlas 无玩家分支](../internal/world/simulation.go) · [原版 Level](https://mappings.dev/1.21.1/net/minecraft/world/level/Level.html) · [原版 LevelChunk](https://mappings.dev/1.21.1/net/minecraft/world/level/chunk/LevelChunk.html)

### 随机刻需要新增的工作

- 原版 `tickChunk` 对每个含有随机刻状态的 16³ 区段抽样 `randomTickSpeed` 次，Java 默认 3。每次从区段全部位置抽取，可以抽到空气或重复位置。先对取出的方块状态执行随机刻，再检查**同一份原始状态**的流体。选点用 `Level.getBlockRandomPos` 独立的 32 位线性同余状态，处理器再使用传入的随机源。测试应固定两种状态并比较坐标、调用顺序、后续随机值。[Wiki 游戏刻](https://minecraft.wiki/w/Tick#Random_tick) · [原版 ServerLevel](https://mappings.dev/1.21.1/net/minecraft/server/level/ServerLevel.html) · [原版 Level](https://mappings.dev/1.21.1/net/minecraft/world/level/Level.html)
- 在本次核验的 JAR 中遍历 26,684 种方块状态，**82 种方块、1,082 种状态**的 `BlockState.isRandomlyTicking()` 为真。其中 16 种状态的 `getFluidState().isRandomlyTicking()` 也为真。Atlas 的 `Section` 已维护非空气计数。可在区段加载、生成和方块变更时维护随机刻资格，先接孤立树叶凋落和一种作物，再扩展到树苗、草方块、铜氧化与熔岩点火。旧存档区段也要参与。[Atlas 区段](../internal/world/chunk.go) · [原版方块状态](https://mappings.dev/1.21.1/net/minecraft/world/level/block/state/BlockBehaviour%24BlockStateBase.html) · [原版流体状态](https://mappings.dev/1.21.1/net/minecraft/world/level/material/FluidState.html)
- 熔岩是检验双重分派的具体场景。`LiquidBlock.randomTick` 本身会委托流体，因此同一位置既能通过方块回调进入熔岩随机刻，又能通过独立流体分支再次进入。测试应记录回调次数和随机数消耗。[原版 LiquidBlock](https://mappings.dev/1.21.1/net/minecraft/world/level/block/LiquidBlock.html)
- 原版区块内还有独立的天气分支。1.21.1 每个合格区块进行 `randomTickSpeed` 次降水尝试，每次以 1/48 的概率进入冰冻、积雪或降水处理。雷击另行判定。用速度 0 和非零值分别验证降水分支，并确认速度 0 时有资格的计划流体刻仍运行。[原版 ServerLevel.tickChunk](https://mappings.dev/1.21.1/net/minecraft/server/level/ServerLevel.html) · [Atlas 计划流体刻](../internal/world/fluid_runtime.go)

## 计划方块刻清点

按 1.21.1 注册名统计，每个方块计一次。原版探针记录声明计划刻方法的类，再与 Atlas `TickScheduledBlock` 的实际分支交叉核对。同一个方法的不同注册名可能有不同支持情况，例如生长植物基类名下只有海带头和海带植株已接入。[原版方法清单](../internal/worldgen/block_tick_handlers_1.21.1.json) · [Go 分派](../internal/worldgen/block_tick.go) · [海带判定](../internal/worldgen/kelp_shape.go)

| 分派结果 | 注册名数 | 含义 |
| --- | ---: | --- |
| 继承原版空计划刻 | 920 | 收到该类型的计划刻可直接结束。它仍可能有随机刻、邻居回调或方块实体行为。 |
| 已接入非空计划刻 | 40 | 树叶 10、气泡柱及两种支撑方块 3、普通 FallingBlock 处理器 23、可疑方块 2、海带 2。这里统计到计划回调为止，例如铁砧可开始下落，实体阶段仍缺伤害及落地处理。 |
| 尚未接入的非空计划刻 | **100** | 包括按钮、压力板、中继器、比较器、观察者、珊瑚、火、耕地、脚手架、滴水石、幽匿感测体等。可按原版方法声明类分批实现，并分别检查触发入口。 |
| 合计 | 1,060 | 只衡量计划方块刻分派。 |

## 建议的实现顺序

| 顺序 | 可独立交付的改动 | 验收标准 |
| --- | --- | --- |
| 1 | 修通砍树触发的叶距更新，将石质按钮松开移入世界计划队列。两项可独立提交。 | 重现上文场景：断开唯一原木，下一刻叶距变为 7；石按钮第 19 刻仍按下，第 20 刻松开。另测松开前保存重启、替换按钮、区块暂停与恢复。 |
| 2 | 分开计划刻、区块随机刻、实体与方块实体的资格。接入出生、强加载和传送门票据，补主世界共享时间及规则。 | 无玩家的强加载区块仍会执行到期流体刻，远离非旁观玩家的同一区块不长作物；两个玩家重叠时每区块每刻只执行一次。 |
| 3 | 接入区段随机刻与首批处理器，补齐树叶从距离更新到自然凋落的完整路径。 | 在固定选点和处理器随机源下，逐刻比对坐标、叶距、掉落与作物年龄。检查加载旧区段、跨区块修改、速度 0 和同坐标方块／流体的顺序。 |
| 4 | 分批扩展计划处理器、普通邻接通知、红石供电和方块事件。 | 用珊瑚失水、按钮驱动中继器、观察者脉冲、活塞作动的原版冻结世界逐刻对照方块和队列。 |
| 5 | 补实体迁移与掉落物生命周期、方块实体 ticker、天气与自然生成，再扩展 AI、袭击、龙战与游戏事件监听。 | 分别用越过 x=511/512 的实体、漏斗接熔炉、营火冷却、降雪及受上限约束的刷怪场景验收；同时核对客户端可见状态和重启后的 NBT。 |

## 复核依据

本次比对使用官方服务端包 SHA-1 `59353fb40c36d304f2035d51e7d6e6baa98dc05c`，解包后的服务端 JAR SHA-1 为 `a8ae1f986e20cc91442b6b2b64d62bcd065618d7`。Wiki 用于辨别玩家可见机制及版本历史。阶段、资格和注册表数量依据 1.21.1 字节码与探针。可在仓库根目录用 JDK 21+ 复查。[官方版本元数据](https://piston-meta.mojang.com/v1/packages/c086fab6229047df03bb78951c7a1b8fce8c4892/1.21.1.json) · [官方服务端映射](https://piston-data.mojang.com/v1/objects/03f8985492bda0afc0898465341eb0acef35f570/server.txt) · [版本与哈希校验](../tools/atlas_tools.py)

```powershell
python tools/check_env.py --reference
python tools/worldgen/build_block_tick_probe.py --check
javap -p -c -classpath artifacts/reference-java/server-1.21.1.jar aqu aqs aqb dcw dgi dki
go test ./internal/server ./internal/world ./internal/worldgen -run '^Test(WorldClock|PlayerSimulation|LiveTickRetriesFailedBlockWhileSandAndWaterAdvance|LiveLeafDistanceUpdatesFlushAtWorldSave|LeafScheduledTicksAgainstVanilla)' -count=1
```

`aqu`、`aqs`、`aqb`、`dcw`、`dgi`、`dki` 分别对应 ServerLevel、ServerChunkCache、ChunkMap、Level、ButtonBlock、LeavesBlock。随机刻数字的复算方法是按状态 ID 0 至 26,683 取 `Block.stateById`，对 `state.isRandomlyTicking()` 计数并按 `state.getBlock()` 去重，同时独立统计 `state.getFluidState().isRandomlyTicking()`。这些数值可以作为后续抽取状态表时的完整性检查。[原版状态探针入口示例](../tools/worldgen/probes/BlockTickLevel.java)

本次重跑了上面的计划方块原版探针和 7 项既有 Go 定向测试，另以 2 个临时场景观测当前差异。新增的随机刻、天气、实体与红石场景列作后续验收；本次没有运行全套测试或为这些子系统给出一致率。
