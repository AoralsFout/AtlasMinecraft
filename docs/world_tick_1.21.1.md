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

玩家驱动的世界刻现已在计划流体刻之后、实体刻之前运行区段随机抽样。默认每个有资格的区段每刻抽三次，使用存档的 `randomTickSpeed`。区段资格来自 1.21.1 全部 26,684 个状态的原版标记。实际方块回调已接入十种树叶、耕地和小麦。耕地在被抽中时按附近的水、上方是否有雨和作物标签更新。顶部实心遮挡通过延迟一刻的计划更新转为泥土。降水造成的冰雪在同一区块的区段抽样之前更新。其他作物与熔岩起火仍待实现。[世界刻入口](../internal/world/tick.go) · [抽样与小麦实现](../internal/world/random_tick.go)

晴天与雨天的原版回放逐刻比较方块状态、区段和降水抽样坐标、坐标生成器和关卡随机源的结束状态。树叶另以原版掉落表连续结算 5,120 次，Go 回放在中途写出并重新读取命名随机序列。联机刻入口测试从移除含水树叶旁的原木开始，验证旁观者、速度为零、凋落后留下水源，以及重开存档后的掉落实体。[逐刻场景与复查方法](worldgen/random_ticks_1.21.1.md) · [树叶掉落与存档](worldgen/leaf_decay_1.21.1.md)

耕地新增 16 个原版场景，覆盖跨区块灌溉、雨雪差异、覆盖与撤除，以及相同随机条件下灌溉对小麦的影响。存档测试通过玩家放置入口覆盖耕地，重开后按剩余 1 刻执行。实体踩踏与耕地变成完整泥土时的实体抬升留在实体碰撞阶段。[耕地实现](../internal/world/farmland.go) · [存档测试](../internal/world/farmland_test.go)

## 无玩家时的强加载区块

`/forceload add|remove <x z> [x z]` 接受方块坐标，两个端点都计入范围，每次最多操作 256 个区块。`query [x z]` 和 `remove all` 作用于当前维度，也可以通过 `/execute in <dimension> run forceload ...` 操作其他维度。命令成功后票据已存入该维度的 `data/chunks.dat`，区块由后台加载。[命令入口](../internal/server/forceload.go) · [存档读写](../internal/world/forced_chunks.go)

一个强加载中心提供等级 31 的票据。中心可运行实体刻；中心及周围一圈可运行计划方块刻和流体刻；再外一圈提供已加载的邻区。多个来源覆盖同一区块时取最强等级，移除其中一个来源会保留其他来源的资格。随机刻另查是否有非旁观玩家在区块中心 128 格以内，边界取严格小于。[票据合并与阶段资格](../internal/world/simulation.go)

Java 1.21.1 专用服务端对照使用无玩家平坦世界，种子 1234，`spawnChunkRadius=0`。冻结后执行 `forceload add -1600 1600`，得到区块 `[-100,100]`。在有边墙、露天的石底上设置水和灌溉的小麦，把 `randomTickSpeed` 调到 4096，步进 20 刻：水抵达相邻格，小麦仍是年龄 0。保存重启后可查询和移除该票据。仓库保留原版写出的 NBT 样本，Go 测试直接打开它并检查更新后的坐标编码。[原版样本](../testdata/worldgen/forced_chunks_1.21.1.dat) · [存档兼容测试](../internal/world/forced_chunks_test.go)

Atlas 的服务器入口测试另检查玩家离开后的水流计时：5 刻的计划更新先走 2 刻，保存重启后再走 3 刻流出。移除最后一张票据后，新开的出水口保持干燥，空闲卸载可释放整个 5×5 邻域。[服务器回归测试](../internal/server/forceload_test.go)

## 主世界出生区块

主世界从 `level.dat` 读取出生点与 `GameRules.spawnChunkRadius`。省略规则时取 2，设为 0 时撤销出生票据。`/setworldspawn` 跨区块移动出生点或 `/gamerule spawnChunkRadius` 改变半径后，重算受影响的加载范围。每个区块取出生、玩家和强加载票据中的最强等级。离线打开存档不会单独触发出生区块生成，服务器首次世界刻会从存档恢复这张票据。[模拟入口](../internal/world/simulation.go) · [半径与存档](../internal/world/spawn_chunks.go)

在 Java 1.21.1 无玩家平坦世界中，依次执行下表命令。每组稳定后用 `/perf start` 和 `/perf stop` 取出 `server/levels/minecraft/overworld/chunks.csv`，按坐标核对所有达到 FULL 或更高级别的区块。三组分别有 49、74、25 个区块符合条件。

| 操作 | 实体刻区块 | 仅方块刻区块 | 仅加载区块 |
| --- | ---: | ---: | ---: |
| 保留默认半径 2，执行 `/setworldspawn -1599 64 1601` | 9 | 16 | 24 |
| 在原地 `/forceload add -1599 1601`，再执行 `/setworldspawn 1601 64 1601` | 10 | 24 | 40 |
| 执行 `/gamerule spawnChunkRadius 0`，保留旧址的强加载 | 1 | 8 | 16 |

在默认半径的另一轮原版实验中，冻结世界，在出生区块以及沿 X 方向相距 2、3 个区块的位置放置悬空沙。在露天灌溉耕地上种下年龄 0 的小麦，将 `randomTickSpeed` 设为 4096。步进 20 刻后，中心与距离 2 处的沙已离开原位，距离 3 处的沙仍悬空，小麦年龄仍为 0。

原版 `ServerLevel.tick` 在维度连续没有玩家和强加载票据时，仅在前 300 刻进入实体阶段。Atlas 以落沙验证第 300 刻实体仍移动，第 301 刻起暂停。此时新放置的悬空沙仍能按计划刻转为实体。添加强加载票据会恢复实体阶段。出生票据本身也不会给区块提供随机刻所需的玩家距离条件。[区块与实体边界测试](../internal/world/spawn_chunks_test.go) · [命令与重启测试](../internal/server/world_tick_test.go) · [Mojang 出生半径说明](https://www.minecraft.net/en-us/article/minecraft-java-edition-1-20-5)

## 下界传送门目的地票据

玩家成功穿过下界传送门后，目的地维度获得中心等级为 30 的临时票据。沿用已有门时，中心取搜索命中的门方块所在区块，即使碰撞修正后的落点在邻区块。新建门时，中心取实体实际落点所在区块。相同区块最近一次穿越决定该区块的保活期限，服务器重启会清除临时票据。[目的地选择](../internal/world/portal_forcer.go) · [玩家穿越](../internal/server/portals.go) · [票据生命周期](../internal/world/portal_tickets.go)

在 Java 1.21.1 无玩家专用服务端关闭出生区块，强加载下界入口与主世界目的地。在下界 `200.5 80.4 200.5` 生成掉落物使其穿越，再让第二个掉落物经同一座门抵达。冻结世界并逐段步进，用 `/perf` 的主世界 `chunks.csv` 检查区块 `[100,100]`。两次抵达的票据键均为门方块 `(1601,80,1600)`，创建刻从 3 更新到 105。

| 距最近一次抵达 | 原版区块报告与方块状态 |
| --- | --- |
| 299 刻 | 中心仍为 PORTAL 30。在距离中心 1、2 个区块处放置悬空沙。 |
| 300 刻 | 中心仍为 PORTAL 30，沙仍在原位。 |
| 301 刻 | 两处沙都完成计划刻并离开原位，中心回落到 FORCED 31。第一圈落沙实体的 `Time` 为 0。此刻在第二圈再放沙，步进两刻仍悬空。 |

仓库保留了原版报告中的中心等级与各圈数量。回归测试按相同时间点对照，完整区块数在第 301 刻由 49 个降为 25 个。[1.21.1 原版报告摘录](../testdata/worldgen/portal_ticket_expiry_1.21.1.csv)

这个边界来自原版的刻内顺序。计划方块刻和流体刻先运行，随后清除年龄大于 300 的票据，实体阶段使用降级后的范围。Go 回归同时覆盖出生、玩家、另一座门和强加载票据重叠，以及无人、续期与重启。原版实验使用掉落物触发穿越，Atlas 的联机入口测试使用玩家；普通实体跨维度迁移仍待实现。[到期边界测试](../internal/world/portal_tickets_test.go) · [跨区块门框测试](../internal/world/portal_forcer_test.go) · [服务器入口测试](../internal/server/portals_test.go)

### 玩家穿过末地传送门

用协议 767 客户端连接 1.21.1 官方独立服务端，关闭出生区块加载，强加载入口、出口和返回点。用 `/tick freeze` 和 `/tick step` 步进，并从 `/perf` 的维度区块报告读取票据。玩家从主世界以偏航 12°、俯仰 21° 进入，抵达末地平台时的位置为 `(100.5, 49, 0.5)`，偏航 90°、俯仰 21°。票据键为脚下的方块 `(100, 49, 0)`，中心区块为 `[6, 0]`。

| 原版采样时刻 | 末地中心区块的票据 |
| --- | --- |
| 首次进入，票据时钟 89 | PORTAL 30 |
| 再次进入，票据时钟 204 | 同一位置的 PORTAL 30 续期 |
| 续期后第 300 刻 | PORTAL 30 |
| 续期后第 301 刻 | FORCED 31 |

首次离开末地时服务端发送游戏事件 4，等客户端发回 `PERFORM_RESPAWN` 后转移玩家。原版在玩家存档记录 `seenCredits`，以后离开末地可直接返回。两次返回后，主世界目标区块都只保留原有的 FORCED 票据。Atlas 的往返测试还覆盖了从下界进入末地、观看终末之诗期间移出模拟范围，以及重复确认。[服务器往返回归](../internal/server/portals_test.go)

下表取自原版服务端发给测试客户端的位置同步包。共享出生点设在 `(-1592, 75, -1592)`，`spawnRadius=0`。

| 返回目标 | 实际位置 | 偏航 / 俯仰 |
| --- | --- | --- |
| `/spawnpoint EndProbe -1592 70 -1592 37` | `(-1591.5, 70.1, -1591.5)` | 37° / 0° |
| 在该位置放置石头 | `(-1591.5, 71, -1591.5)`，收到无可用重生点事件 | 0° / 0° |
| 床头在 `(-1592, 67, -1592)`，朝北 | `(-1590.5, 67, -1591.5)` | 89.999992° / 0° |
| 下界重生锚在 `(160, 240, 160)` | `(160.5, 240, 159.5)` | 约 −0.000005° / 0° |

另用测试客户端向原版服务端发送方块使用包，将有两格能量的锚设为个人重生点。第一次经终末之诗返回后仍有两格，第二次直接返回后剩一格。Atlas 读取原版床和重生锚的个人重生点记录，并在目标地形准备完毕后提交扣能量操作。睡床、右键绑定重生锚等玩家交互仍待接入。[落点与扣能量回归](../internal/world/player_respawn_test.go)

## 三维度共享时间与日夜循环

主世界每个游戏刻推进一次 `Time`。下界和末地读取同一时钟，各维度的方块和流体计划队列仍按游戏刻运行。`doDaylightCycle=false` 只停止 `DayTime` 的自动递增，三个维度仍可执行 `/time set` 和 `/time add`。新建世界的两个计数器从零开始。读取缺少 `DayTime` 的旧记录时使用其中的 `Time`。

在原版专用服务端冻结世界，从游戏年龄 1 开始步进。在每行操作后分别从三个维度执行 `time query gametime|daytime|day`，结果一致。表内日时为完整的 `DayTime`，其中 24003 对应第 1 天的第 3 刻。

| 操作 | 游戏年龄 | 日时 |
| --- | ---: | ---: |
| 设置日时 23999 | 1 | 23999 |
| 步进一刻 | 2 | 24000 |
| 关闭日夜循环，步进一刻 | 3 | 24000 |
| 在下界执行 `time add 3` | 3 | 24003 |
| 依次设置日时 0、42 | 3 | 0、42 |
| 开启日夜循环，步进一刻 | 4 | 43 |

协议 767 的时间包包含两个有符号长整数。日夜循环停止时，服务端把日时取负，零编码为 −1。日时为 42 时，三次跨维度传送收到的日时均为 −42。周期同步每 20 个服务器刻发生一次，主世界在该刻推进前发包，下界和末地在主世界推进后发包。三名客户端同期收到的前两组游戏年龄分别为 `(5, 6, 6)`、`(25, 26, 26)`。[四轮原版时间包](../testdata/protocol/world_time_1.21.1.csv) · [逐刻与计划刻回归](../internal/server/world_tick_test.go)

互读实验使用同一份存档副本。原版关闭日夜循环并保存日时 43，Atlas 打开后登录包读到 −43；在 Atlas 将日时设为 12345 并保存，再用原版重开，三个维度均读到 12345，规则仍为 false。

## 天气计时、降雪与结冰

主世界在计划刻与区块刻之前推进天气。强制晴天结束后，雷暴和降雨分别倒计时。正数减到零的当刻翻转标志，下一刻才抽取新时长。原版对未激活状态抽取 12000–180000 刻，对已激活的降雨抽取 12000–24000 刻，对已激活的雷暴抽取 3600–15600 刻，端点均包含在内。`/weather` 支持刻、秒和天；省略时长时使用对应的原版分布。`doWeatherCycle=false` 冻结三项计时，手动设置的天气仍会逐刻改变雨势和雷势。[天气实现](../internal/world/weather.go) · [原版方法索引](https://mappings.dev/1.21.1/net/minecraft/server/level/ServerLevel.html)

协议对照使用原版专用服务端和两个协议 767 客户端。在 `/tick freeze` 后逐次执行 `/tick step 1`，确认游戏年龄前进一刻，再用 `/save-all flush` 读取五个天气 NBT 字段。49 行记录包含命令、单刻、雨势变化中途登录和重启。CSV 最后两列按发送顺序记录主世界和下界收到的天气包载荷，每个载荷占五字节。第 20 刻雨势跨过 0.2，主世界先收到雨势、雷势变化包，再与下界一起收到开始降雨和两项强度；反向跨过阈值时收到停止降雨。中途登录包的雷势为当刻雷势乘雨势。[原版包及存档记录](../testdata/protocol/weather_1.21.1.csv) · [Atlas 重放测试](../internal/server/weather_test.go)

互读实验从原版关闭天气循环、保存剩余 37 刻的雨天开始。Atlas 打开后，登录包中的雨势为 1、雷势为 0。在 Atlas 改为 23 刻雷暴并保存，再用原版重开，`rainTime` 和 `thunderTime` 均为 23，规则仍关闭，登录包中的两项强度均为 1。存档保留天气标志与计时器，重开时按标志重建强度。

降水沿用每次 1/48 的列抽样。取 `MOTION_BLOCKING` 顶部的生物群系，在水面和积雪位置分别计算温度并读取方块光照。寒冷、方块光照小于 10 的水源，只要四邻有一格不含水就能冻结。积雪还要求当地已进入降雨状态且下方可承托，默认最多一层，`snowAccumulationHeight` 可以关闭堆积或使其逐次增至最多八层。13 个原版列场景覆盖晴天岸边、湖心、流水、含水树叶、屋顶、积雪承托、草方块外观与光照边界。运行 `python tools/worldgen/build_random_tick_probe.py --check` 可复采原版结果。[原版冰雪场景](../testdata/worldgen/precipitation_1.21.1.json) · [区块刻回放测试](../internal/world/precipitation_test.go)

## 自然雷击、避雷针与闪电实体

雷暴期间，每个合格区块在降水抽样前有 1/100000 的雷击机会，`randomTickSpeed=0` 也会触发。Atlas 从抽中的列取得 `MOTION_BLOCKING` 高度，在三维半径 128 格内寻找最近的露天避雷针。找不到避雷针时，从列周围能见天空的生物和玩家中抽取落点；当地能够下雨才生成闪电。闪电在首次实体刻使避雷针通电，发送火花事件，并登记八刻后的断电计划。区块卸载会移除闪电，存档保留避雷针状态和断电计划。[落点与实体刻](../internal/world/lightning.go) · [原版 LightningBolt 方法索引](https://mappings.dev/1.21.1/net/minecraft/world/entity/LightningBolt.html)

在原版 1.21.1 平坦世界锁定雷暴，设置 `randomTickSpeed=0`，并关闭生物生成和起火。6000 刻内记录到两次自然雷击，都落在露天避雷针上方的 (8.5, −58, 8.5)。另一根上方有玻璃的避雷针未被选中。另一次冻结步进中，召唤闪电后避雷针在第 1 至 8 刻保持通电，第 9 刻断电。协议测试按字节比较原版闪电生成包的类型、坐标、角度和速度。固定种子的原版区块刻探针还分别打开、关闭 `doMobSpawning`，核对雷击之后的降水尝试、区段坐标及两条随机流的末态。[原版抽样](../testdata/worldgen/lightning_1.21.1.json) · [区块刻与存档回放](../internal/world/lightning_test.go) · [客户端发包回归](../internal/server/falling_entities_test.go)

固定种子场景将 `randomTickSpeed` 设为 54。关闭生物生成时，两端都没有抽中降水列；开启后，陷阱马判定消耗一次 `nextDouble()`，两端都在同一列命中一次降水，后续 54 个区段坐标也逐一相符。

## 自然雷击生成骷髅马陷阱

自然雷击确定最终落点后，`doMobSpawning=true` 会从关卡随机源取一次 `nextDouble()`。抽值严格小于落点区域难度的 1% 且落点正下方没有避雷针时，原版先在整数方块坐标生成成年骷髅马，再在该格底面中心生成仅作视觉效果的闪电。初生马生命值为 15，`SkeletonTrap=1`、`SkeletonTrapTime=0`。避雷针保留随机抽样，关闭生物生成则跳过抽样；两者都保留普通闪电。[生成与存档实现](../internal/world/skeleton_trap.go) · [原版区域难度](https://mappings.dev/1.21.1/net/minecraft/world/DifficultyInstance.html)

逐刻对照直接调用 1.21.1 服务端 JAR 的区块刻、局部难度查询和实体构造器。探针固定候选落点，分别改变基础难度、日时、月相、区块 `InhabitedTime`、落点方块及游戏规则。每个场景运行四刻，第 2 刻命中自然雷击，后两刻连续使用该随机源。Go 回放通过世界刻入口运行，并在重开世界后检查陷阱马、区块居住时间和游戏规则。[11 组原版逐刻记录](../testdata/worldgen/skeleton_traps_1.21.1.json) · [世界刻回放](../internal/world/skeleton_trap_test.go)

| 同一次约 6.49465% 的抽签 | 区域难度 | 马与闪电的生成结果 |
| --- | ---: | --- |
| 困难、满月、长期居住 | 6.75 | 陷阱马，随后生成视觉闪电 |
| 同条件改为新月 | 6.00 | 普通闪电 |
| 满月，落点下方放置避雷针 | 6.75 | 普通闪电，消耗抽签 |
| 满月，关闭 `doMobSpawning` | 6.75 | 普通闪电，跳过抽签 |

## 骷髅马陷阱的触发与超时

陷阱使用玩家与马脚下坐标的三维距离。距离平方严格小于 100 时，创造模式玩家可以触发，旁观玩家跳过。马在第一个实体刻就能检查目标。此后，当实体年龄与运行时实体 ID 之和为偶数时，马才检查新目标。效果由区块的实体刻资格决定。未获得实体刻的马暂停计时。

在官方 1.21.1 专用服务端执行 `/tick freeze`，关闭 `doMobSpawning` 并设为午夜。给初始马加上 `SkeletonTrap:1b`、`NoGravity:1b` 和用于查询的标签。每步相对于马传送玩家，执行 `/tick step 1`，等待世界时间前进一刻，查询马的陷阱标志和计时，同时记录协议 767 的实体生成及骑乘包。[逐刻采样记录](../testdata/protocol/skeleton_trap_activation_1.21.1.json)

| 原版场景 | 逐刻观测 |
| --- | --- |
| 水平或垂直靠近，马 ID 2 | 水平 10 格保持两刻，10.001 格仍未触发。第 4 刻旁观玩家垂直距离 9.999 格，第 5 刻切为创造，均未触发。第 6 刻触发，计时停在 5。 |
| 对角线靠近，马 ID 17 | 水平偏移 (6, 8) 时保持两刻。偏移改为 (5.999, 8) 后，第 3 刻触发。 |
| 首刻已经靠近，马 ID 35 | 玩家在 9 格内，马在第 1 刻触发，计时保持 0。 |
| 超时，马 ID 54 | 初值 17998，玩家距离 12 格。逐刻得到 17999、18000、实体消失。 |
| 超时刻靠近，马 ID 55 | 初值 18000，玩家距离 9 格。马在首次实体刻先触发，原马保留，计时保持 18000。 |

触发刻保留原马的 UUID，将原马设为成年、驯服并解除陷阱。客户端依次收到视觉闪电、原马的骷髅骑手，随后三次收到新骷髅马及其骑手。八个新增实体的类型序列为 `64, 91, 92, 91, 92, 91, 92, 91`。四组骑乘关系均在坐骑和骑手可见后建立。逐刻记录保留生成包顺序，骑乘关系合并了原版追踪器重复发送的同一关联。重开世界后，四匹马各自通过 Anvil 的 `Passengers` 保存一个骑手。[世界刻与重开回归](../internal/world/trap_activation_test.go) · [服务端包顺序回归](../internal/server/trap_activation_test.go)

骑手实体还需要移动、受伤与战斗 AI 的逐刻对照。

## 普通闪电起火

| 实体刻 | 和平、简单 | 普通、困难 |
| --- | --- | --- |
| 首次闪光 | 跳过点火 | 先尝试闪电所在方块，再尝试四个随机位置。每次独立抽取 X、Y、Z 三个偏移，各为 −1、0 或 1，可以重复。 |
| 同一道闪电的后续闪光 | 尝试中心一次 | 尝试中心一次 |

每次闪光都读取当前的 `doFireTick`。规则关闭或闪电仅有视觉效果时，跳过点火及偏移抽样。点火以闪电坐标向下取整，只替换空气。灵魂沙和灵魂土上生成灵魂火。普通火可以立在牢固的上表面或可燃方块上，也可以附着在可燃侧面和顶面。含水树叶不能提供这种附着。[闪电实体刻](../internal/world/lightning.go) · [火焰放置状态](../internal/worldgen/nether_survival.go)

普通火放置后从关卡随机源抽取 30–39 刻的计划延迟。闪电的偏移与闪光间隔使用实体自身的随机源。计划刻从存档队列调度，处理火龄、雨水熄灭、相邻燃料烧毁及向空气蔓延。灵魂火无此计划刻。有效黑曜石框架沿用现有传送门检测。TNT 被烧后的点燃实体和爆炸仍待实现。[火焰计划刻](../internal/world/fire_tick.go)

原版探针实际执行 1.21.1 服务端的 `LightningBolt.tick` 与 `FireBlock.tick`。17 组共 459 个世界刻记录了火焰坐标及完整状态、闪光寿命和两路随机源状态。三次补闪的样本在每次点燃后移除中心火块，并在第 5、13、19 刻观察到中心再次起火。水源样本在首刻只点燃邻格，第 6 刻流水进入邻格并熄火。另有三组跟踪至第 120 刻。固定种子的雨天样本在第 33 刻熄灭一处火，木地板样本在第 108 刻由两处火增至四处。另用原版冻结专用服逐刻召唤普通闪电，观察到普通难度在石面首刻起火，困难难度在灵魂沙上首刻生成灵魂火，简单和和平两次独立观测分别在第 6 和第 5 刻首次起火。后两个刻数取决于这道闪电的随机间隔。[原版逐刻记录](../testdata/worldgen/lightning_fire_1.21.1.json) · [世界刻回放](../internal/world/lightning_fire_test.go)

## 闪电对铜块除锈

原版只在闪电实体的第一刻除锈。直接命中时取 `floor(x), floor(y - 0.000001), floor(z)`，因此闪电落在整数高度时，起点在其脚下。命中避雷针时先通电，再取针尖反方向相邻的一格。横放的针从侧面支撑块开始，倒挂的针从上方开始。支撑块是涂蜡铜或石头时，周围的铜保持原样，关卡随机源也不会因除锈而前进。和平难度、关闭 `doFireTick` 以及骷髅马陷阱的视觉闪电都可以触发除锈。[世界刻实现](../internal/world/lightning.go) · [铜块阶段表](../internal/world/copper_weathering_1.21.1.json)

起点的未涂蜡铜一次恢复到未氧化阶段，保留方块属性。关卡随机源随后抽取 3 至 5 条路径，每条都从起点重新出发，尝试走 1 至 8 步。每步最多抽取 10 个候选位置，按 X、Y、Z 顺序分别抽取 −1、0、1 的偏移。遇到首个未涂蜡铜就前进到该格，退一个氧化阶段，并发送数据为 −1 的电火花事件 3002。已经干净的铜仍可被选中并发出火花，同一位置可以重复命中。连续 10 次落空仅结束当前路径。[随机路径实现](../internal/world/lightning_copper.go)

1.21.1 服务端探针执行原版 `LightningBolt.tick`，记录了 16 个场景、216 个世界刻、574 个候选位置和 308 次路径火花。场景覆盖六个针尖朝向、跨区块、铜门双半部、点亮的铜灯、首刻先起火再除锈，以及后续补闪。密集铜块样本检查 46 个候选位置，发出 28 次路径火花。Go 测试按顺序核对每个候选坐标和客户端事件，并逐刻比较完整方块状态、两路随机源和实体移除时机。可用 `python tools/worldgen/build_random_tick_probe.py --check` 重新采样。[原版逐刻记录](../testdata/worldgen/lightning_copper_1.21.1.json) · [回放测试](../internal/world/lightning_copper_test.go)

另在冻结的原版专用服用 `tick step 1` 复核了直接命中、朝东和倒挂避雷针、涂蜡支撑块四种布置。未涂蜡的上半铜台阶在首刻恢复原色并保持上半状态，避雷针在第 9 刻解除通电。自然雷暴中，客户端两次收到位置为 `(5.5, -51, 5.5)` 的闪电生成包。朝东的避雷针位于 `(5, -52, 5)`，其西侧支撑台阶恢复原色，远处的对照铜块仍完全氧化。

## 闪电命中普通生物

普通闪电在每个有效闪光刻重新查询存活目标。命中箱以闪电的精确坐标为准，X、Z 各向两侧延伸 3 格，Y 向下 3 格、向上 9 格。目标碰撞箱必须与它有重叠体积。刚好贴边的目标不会受击，中心在外但身体伸入的目标会受击。补闪时重新取目标，已命中过的生物可以再被命中。陷阱视觉闪电跳过整个伤害查询。[世界刻入口](../internal/world/lightning.go) · [碰撞与实体更新](../internal/world/lightning_damage.go)

普通命中的基础伤害为 5 点。固定种子样本中，一只初始生命值为 40 的无甲牛在第 1、2、4、5、7、8、11、12 刻被命中，只有第 1 和第 11 刻各扣 5 点。伤害冷却为 20 刻，剩余时间大于 10 刻时只接受比上一次更高的伤害差值。晴天下同一样本在第 29 刻才因持续燃烧再扣 1 点。闪电直击受护甲减免，持续燃烧伤害绕过护甲。生物每次受击先将 `Fire` 加一，结果恰为 0 时才点燃 8 秒，因此初始 `Fire=-1` 和 `Fire=0` 分别得到 160 和 1。[伤害与着火实现](../internal/world/living_damage.go)

原版探针调用 1.21.1 的 `LightningBolt.tick`、`Entity.thunderHit` 和 `LivingEntity.baseTick`，从服务端资源包加载伤害标签。8 个场景各观测 45 刻、10 个目标，记录了 280 次命中。回放逐刻核对命中名单、生命、剩余火刻、受伤动画、伤害冷却、吸收生命和同步给客户端的着火标志。目标包括移入或离开范围的牛、无敌牛、雨中的牛，以及戴铁头盔和穿下界合金胸甲的牛。另在冻结的原版专用服分别于晴天和雨天观测 22 刻，验证通过 NBT 召唤的静止牛从 `Fire=0` 受击后变为 1。Atlas 另有存档重开、坐骑死亡后骑手保留和可见客户端收包的回归测试。[原版记录](../testdata/worldgen/lightning_hits_1.21.1.json) · [世界回放](../internal/world/lightning_damage_test.go) · [客户端包测试](../internal/server/lightning_damage_test.go)

下一阶段可对照猪和村民的实体替换、哞菇对同一道闪电的去重，并验证转化后的生成包与存档。在线玩家的受伤流程、死亡掉落及各物种的特殊姿态碰撞箱仍需单独接入或验证。

多个等距避雷针或多个生物候选的原版遍历顺序仍待对照。其他天气联动包括炼药锅承接降水、积雪增厚顶起实体，以及冰雪受高方块光照融化。

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

## 调查基线中的区块执行资格

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
