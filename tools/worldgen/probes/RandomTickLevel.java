import java.lang.reflect.Field;
import java.util.*;
import java.util.concurrent.atomic.AtomicLong;
import sun.misc.Unsafe;

/** Isolated chunk I/O for the original 1.21.1 ServerLevel.tickChunk. */
public final class RandomTickLevel extends aqu {
    private static final int CX = -3, CZ = 2;
    private static final jd CENTER = new jd(CX * 16 + 8, 69, CZ * 16 + 8);
    private dvi chunk;
    private eyq<dfy> blockTicks;
    private eyq<epd> fluidTicks;
    private int time, light;
    private Map<jd,dtc> border;
    private boolean rainy;
    private jm<ddw> biome;
    private jz<ddw> biomes;
    private long order;
    private List<int[]> samples, precipitation;
    private LinkedHashSet<jd> tracked;

    // Servers normally allocate many unrelated managers. The launcher allocates this
    // instance without its constructor, then setup provides the fields under test.
    public RandomTickLevel() {
        super(null, null, null, null, null, null, null, false, 0L, null, false, null);
    }

    private static Field field(Class<?> type, String name) throws ReflectiveOperationException {
        Field field = type.getDeclaredField(name);
        field.setAccessible(true);
        return field;
    }

    private void reset() throws Exception {
        Unsafe unsafe = (Unsafe) field(Unsafe.class, "theUnsafe").get(null);
        chunk = (dvi) unsafe.allocateInstance(dvi.class);
        dcd pos = new dcd(CX, CZ);
        dvj[] sections = new dvj[24];
        // Obtain SECTION_STATES by reflection because its class and a field
        // in the obfuscated outer class both use the name d.
        var strategy = (dvq.d) Class.forName("dvq$d").getField("d").get(null);
        for (int i = 0; i < sections.length; i++) {
            sections[i] = new dvj(new dvq<>(dfy.q, dga.a.o(), strategy), null);
        }
        field(duy.class, "d").set(chunk, pos);
        field(duy.class, "l").set(chunk, dcy.e(-64, 384));
        field(duy.class, "m").set(chunk, sections);
        field(dcw.class, "s").set(this, new eqh(this));
        blockTicks = new eyq<>(ignored -> true, () -> bnc.a);
        blockTicks.a(pos, new eyo<>());
        fluidTicks = new eyq<>(ignored -> true, () -> bnc.a);
        fluidTicks.a(pos, new eyo<>());
        tracked = new LinkedHashSet<>();
        border = new HashMap<>();
        rainy = false;
        time = 0;
        order = 0;
        t = 1779033703;
    }

    public static Map<String,float[]> entitySizes() {
        Map<String,float[]> result = new TreeMap<>();
        for (bsx<?> type : lt.f) {
            result.put(lt.f.b(type).toString(), new float[]{type.l(), type.m()});
        }
        return result;
    }

    public static int[] stateFlags() {
        int[] flags = new int[dfy.q.c()];
        for (int i = 0; i < flags.length; i++) {
            dtc state = dfy.a(i);
            epe fluid = state.u();
            flags[i] = (state.v() ? 1 : 0) | (fluid.f() ? 2 : 0) | (fluid.c() ? 0 : 4)
                    | (fluid.a(awk.a) ? 8 : 0) | (state.a(awe.cy) ? 16 : 0);
        }
        return flags;
    }

    @Override public boolean ad() { return rainy; }
    private jd strikeTarget;
    private bsr spawnedBolt;
    @Override public boolean ac() { return strikeTarget != null; }
    @Override protected jd b(jd column) { return strikeTarget; }
    private bqo difficulty = bqo.a;
    private long day;
    private dwy dimension;
    private ka registry;
    private List<String> additions;
    private cho horse;
    private String horseDefaults;
    private boolean captureFireWrites;
    private List<int[]> copperReads, copperProbes, copperWrites, sparks;
    private long copperSearchSeed;
    private static final StackWalker CALLERS = StackWalker.getInstance();

    @Override public void a(cmx player, int event, jd pos, int data) {
        if (sparks != null && event == 3002) sparks.add(new int[]{pos.u(),pos.v(),pos.w(),data});
    }

    public static Map<String,Object> copperStages() {
        Map<String,Object> stages = new TreeMap<>();
        for (dfy b : lt.e) {
            if (!(b instanceof dpe)) continue;
            var entry = new LinkedHashMap<String,String>();
            entry.put("first", lt.e.b(dpe.b(b)).toString());
            dpe.a(b).ifPresent(previous -> entry.put("previous", lt.e.b(previous).toString()));
            stages.put(lt.e.b(b).toString(), entry);
        }
        return stages;
    }

    private void setupCopperBlocks(String name, jd strike, boolean fire) {
        var blocks = new LinkedHashMap<jd,dtc>();
        String[] metals = {"oxidized_copper","weathered_cut_copper","exposed_copper_grate",
                "copper_block","waxed_oxidized_copper","oxidized_chiseled_copper","oxidized_cut_copper_slab"};
        for (int x=-2;x<=2;x++) for (int y=-1;y<=1;y++) for (int z=-2;z<=2;z++) {
            if (name.equals("sparse") && (x!=1 || y!=0 || z!=1)) continue;
            if (fire && y==1) continue;
            blocks.put(strike.b(x,y,z),block(metals[Math.floorMod(x+3*y+2*z,metals.length)]));
        }
        dtc target = switch (name) {
            case "waxed" -> block("waxed_oxidized_copper");
            case "stone" -> block("stone");
            case "fresh" -> block("copper_block");
            case "visual" -> block("oxidized_copper_bulb").a(dts.r,true);
            case "door" -> block("oxidized_copper_door").a(dts.u,true);
            default -> block("oxidized_cut_copper_slab").a(dts.bh,duk.a);
        };
        blocks.put(strike,target);
        if (fire) {
            for (int x=-2;x<=2;x++) for (int z=-2;z<=2;z++) {
                if (x==0 && z==0) continue;
                blocks.put(strike.b(x,0,z), (x+z)%2==0 ? block("oxidized_cut_copper_stairs") :
                        block("oxidized_copper_trapdoor").a(dts.u,true));
            }
        }
        if (name.equals("door")) {
            blocks.put(strike.d(),target.a(dts.ae,dty.a));
            blocks.put(strike.e(),block("stone"));
        }
        if (name.startsWith("rod_")) {
            ji face = switch(name) {
                case "rod_down" -> ji.a;
                case "rod_up" -> ji.b;
                case "rod_north" -> ji.c;
                case "rod_south" -> ji.d;
                case "rod_west" -> ji.e;
                case "rod_east", "rod_waxed" -> ji.f;
                default -> throw new IllegalArgumentException(name);
            };
            for (ji direction : ji.values()) blocks.put(strike.a(direction),block("oxidized_copper"));
            blocks.put(strike,block("lightning_rod").a(dkn.a,face));
            blocks.put(strike.a(face.g()),name.equals("rod_waxed") ? block("waxed_oxidized_copper") : target);
        }
        blocks.forEach(this::initial);
    }

    public List<Map<String,Object>> copperCases(ka registries) throws Exception {
        registry = registries;
        dimension = registries.d(lu.aK).a(akr.a("minecraft:overworld"));
        var cases = new ArrayList<Map<String,Object>>();
        for (String name : List.of("dense","sparse","fresh","waxed","stone","visual","boundary","door","ignition",
                "rod_down","rod_up","rod_north","rod_south","rod_west","rod_east","rod_waxed")) {
            reset();
            weatherRules = new dcs();
            boolean fire = name.equals("ignition");
            weatherRules.a(dcs.b).a(fire,(net.minecraft.server.MinecraftServer)null);
            difficulty = fire ? bqo.c : bqo.a;
            jd strike = name.equals("boundary") ? CENTER.b(7,0,0) : CENTER;
            setupCopperBlocks(name, strike, fire);
            var scene = scene(name,false);
            long levelSeed = name.equals("sparse") ? 301 : 95;
            boolean visual = name.equals("visual");
            var rng = new dyz(6);
            field(dcw.class,"z").set(this,new dyz(levelSeed));
            btm bolt = bsx.am.a(this);
            field(bsr.class,"ah").set(bolt,rng);
            bolt.b = rng.g();
            field(btm.class,"g").setInt(bolt,1+rng.a(3));
            bolt.a(visual);
            bolt.e(strike.u()+.5,strike.v()+(name.equals("sparse") ? .25 : 1.0),strike.w()+.5);
            scene.put("position",new double[]{bolt.dt(),bolt.dv(),bolt.dz()});
            scene.put("worldSeed",levelSeed);
            scene.put("seed",6);
            scene.put("visual",visual);
            scene.put("fire",fire);
            scene.put("difficulty",difficulty.a());
            var ticks = new ArrayList<Object>();
            for (time = 1; !bolt.dJ(); time++) {
                copperReads = new ArrayList<>();
                copperProbes = new ArrayList<>();
                copperWrites = new ArrayList<>();
                sparks = new ArrayList<>();
                blockTicks.a(time, 65536, (pos, block) -> {
                    dtc state = a_(pos);
                    if (state.a(block)) state.a(this, pos, z);
                });
                bolt.l();
                if (time==1) scene.put("searchSeed",copperSearchSeed);
                var row = new LinkedHashMap<String,Object>();
                row.put("originReads",copperReads);
                row.put("probes",copperProbes);
                row.put("writes",copperWrites);
                row.put("events",sparks);
                row.put("worldRNG",rawState((dyz)z));
                row.put("boltRNG",rawState(rng));
                row.put("removed",bolt.dJ());
                ticks.add(row);
                copperReads = null;
                copperProbes = null;
                copperWrites = null;
                sparks = null;
            }
            scene.put("ticks",ticks);
            cases.add(scene);
        }
        return cases;
    }

    @Override public ka H_() { return registry; }
    @Override public bqp d_(jd p) { return dimension == null ? new bqp(bqo.a,0,0,0) : super.d_(p); }
    @Override public boolean B(jd p) { return inside(p); }
    @Override public dvi m(jd p) { return chunk; }
    @Override public long aa() { return day; }
    @Override public long Z() { return time; }
    @Override public long ak() { return day; }
    @Override public bqo al() { return difficulty; }
    @Override public dwy D_() { return dimension == null ? super.D_() : dimension; }
    @Override public cpl J() { return cpn.f; }
    @Override public boolean b(bsr entity) {
        if (entity instanceof cho h) horse = h;
        if (entity instanceof btm) spawnedBolt = entity;
        if (additions != null) additions.add(lt.f.b(entity.am()).toString());
        return true;
    }

    public String horseDefaults() { return horseDefaults; }

    @Override public List<aqv> a(java.util.function.Predicate<? super aqv> filter) { return List.of(); }
    @Override public boolean b(int cx,int cz) { return cx==CX && cz==CZ; }

    public static Map<String,int[]> fireOdds() throws Exception {
        var ignite = diy.class.getDeclaredMethod("o",dtc.class);
        var burn = diy.class.getDeclaredMethod("n",dtc.class);
        ignite.setAccessible(true); burn.setAccessible(true);
        Map<String,int[]> result = new TreeMap<>();
        for (dfy block : lt.e) {
            int i=(int)ignite.invoke(dga.cr,block.o()), b=(int)burn.invoke(dga.cr,block.o());
            if (i!=0 || b!=0) result.put(lt.e.b(block).toString(),new int[]{i,b});
        }
        return result;
    }

    public List<Map<String,Object>> fireCases(ka registries) throws Exception {
        captureFireWrites = true;
        // blocks/fire has zero loot pools in 1.21.1. Water still runs the
        // original destruction and flow callbacks in this level without a loot manager.
        field(dtb.class,"aQ").set(dga.cr,ern.a);
        registry = registries;
        dimension = registries.d(lu.aK).a(akr.a("minecraft:overworld"));
        List<Map<String,Object>> cases = new ArrayList<>();
        for (String name : List.of("peaceful", "easy", "normal", "hard", "visual", "disabled",
                                 "switch_rule", "soul_sand", "soul_soil", "unsupported", "side", "wet_leaves", "water",
                                 "rain_ticks","fuel_ticks","paused_ticks","three_flashes")) {
            reset();
            difficulty = switch(name) {case "peaceful" -> bqo.a; case "easy" -> bqo.b; case "hard" -> bqo.d; default -> bqo.c;};
            boolean visual = name.equals("visual");
            boolean enabled = !name.equals("disabled") && !name.equals("switch_rule");
            weatherRules = new dcs();
            weatherRules.a(dcs.b).a(enabled,(net.minecraft.server.MinecraftServer)null);
            rainy=name.equals("rain_ticks"); light=15;
            biome=registries.d(lu.aF).c(akr.a("minecraft:plains")).orElseThrow();
            long worldSeed = rainy ? 2 : 95;
            field(dcw.class,"z").set(this,new dyz(worldSeed));
            for (int x=-1;x<=1;x++) for (int y=-1;y<=1;y++) for (int z=-1;z<=1;z++) {
                dtc state = block("air");
                if (y==-1 && !List.of("side","wet_leaves","unsupported").contains(name))
                    state = block(name.startsWith("soul_") ? name : "stone");
                if (x==0 && y==0 && z==0 && name.equals("water")) state = block("water");
                if (x==1 && y==0 && z==0 && name.equals("side")) state = block("oak_planks");
                if (x==1 && y==0 && z==0 && name.equals("wet_leaves")) state = block("oak_leaves").a(dts.C,true);
                if (name.equals("fuel_ticks") && y==-1) state=block("oak_planks");
                initial(CENTER.b(x,y,z),state);
            }
            Map<String,Object> result = scene(name,false);
            result.put("worldSeed",worldSeed);
            result.put("difficulty",difficulty.a()); result.put("visual",visual);
            long seed = name.equals("three_flashes") ? 6 : 7;
            var rng = new dyz(seed);
            btm bolt = bsx.am.a(this);
            field(bsr.class,"ah").set(bolt,rng);
            bolt.b=rng.g();
            field(btm.class,"g").setInt(bolt,1+rng.a(3));
            bolt.a(visual); bolt.e(CENTER.u()+.5,CENTER.v()+.5,CENTER.w()+.5);
            result.put("seed",seed); result.put("position",new double[]{bolt.dt(),bolt.dv(),bolt.dz()});
            List<Object> ticks = new ArrayList<>();
            boolean scheduled=name.endsWith("_ticks");
            for (time=1;time<=(scheduled?120:40);time++) {
                boolean clearCenter = name.equals("three_flashes") && a_(CENTER).a(dga.cr);
                if (clearCenter) a(CENTER,block("air"),3);
                if (name.equals("switch_rule") && time==2) {enabled=true;weatherRules.a(dcs.b).a(true,(net.minecraft.server.MinecraftServer)null);}
                if (name.equals("paused_ticks") && (time==2 || time==65)) {
                    enabled=time==65;
                    weatherRules.a(dcs.b).a(enabled,(net.minecraft.server.MinecraftServer)null);
                }
                blockTicks.a(time,65536,(pos,block)->{
                    dtc state=a_(pos);
                    if (state.a(block)) state.a(this,pos,z);
                });
                fluidTicks.a(time,65536,(pos,fluid)->{
                    epe state=b_(pos);
                    if (state.b(fluid)) state.a(this,pos);
                });
                if (!bolt.dJ()) bolt.l();
                var row = new LinkedHashMap<String,Object>();
                if (clearCenter) row.put("clearCenter",true);
                row.put("enabled",enabled); row.put("fires",tracked.stream().filter(p->a_(p).a(dga.cr)||a_(p).a(dga.cs)).map(p->cell(p,a_(p))).toList());
                row.put("boltRNG",rawState(rng)); row.put("worldRNG",rawState((dyz)z));
                row.put("life",field(btm.class,"f").getInt(bolt)); row.put("flashes",field(btm.class,"g").getInt(bolt));
                row.put("removed",bolt.dJ()); ticks.add(row);
                if (bolt.dJ() && !scheduled) break;
            }
            result.put("ticks",ticks); cases.add(result);
        }
        captureFireWrites = false;
        return cases;
    }

    public String skeletonDefaults() throws Exception {
        ub data = bsx.aN.a(this).f(new ub());
        for (String key : List.of("UUID","Pos","Rotation")) data.r(key);
        var bytes = new java.io.ByteArrayOutputStream();
        var out = new java.io.DataOutputStream(bytes);
        out.writeByte(10);
        data.a(out);
        return HexFormat.of().formatHex(bytes.toByteArray());
    }

    public List<Map<String,Object>> trapCases(ka registries) throws Exception {
        registry = registries;
        dimension = registries.d(lu.aK).a(akr.a("minecraft:overworld"));
        List<Map<String,Object>> cases = new ArrayList<>();
        for (var spec : new long[][] {
                // difficulty, initial day and inhabited time, rod, spawning, RNG seed of strike tick
                {0,0,0,0,1,134733527495473L}, {1,0,0,0,1,134733527495473L},
                {1,0,0,0,1,254272403652741L}, {2,0,0,0,1,254272403652741L},
                {3,0,0,0,1,254272403652741L}, {2,0,3600000,0,1,254272403652741L},
                {2,1536000,0,0,1,254272403652741L},
                {3,1536000,3600000,0,1,114099472626143L},
                {3,1632000,3600000,0,1,114099472626143L},
                {3,1536000,3600000,1,1,114099472626143L},
                {3,1536000,3600000,0,0,114099472626143L}}) {
            reset();
            difficulty = bqo.values()[(int)spec[0]]; day = spec[1]; chunk.b(spec[2]);
            boolean rod = spec[3] == 1, mobs = spec[4] == 1;
            weatherRules = new dcs();
            weatherRules.a(dcs.e).a(mobs, (net.minecraft.server.MinecraftServer)null);
            biome = registries.d(lu.aF).c(akr.a("minecraft:plains")).orElseThrow();
            rainy = true;
            initial(CENTER,block(rod ? "lightning_rod" : "stone"));
            strikeTarget = CENTER.d();
            Map<String,Object> result = new LinkedHashMap<>();
            result.put("base", spec[0]); result.put("initialDay", day); result.put("initialInhabited", chunk.u());
            result.put("rod",rod); result.put("mobSpawning",mobs);
            List<Object> ticks = new ArrayList<>();
            for (int i=0; i<4; i++) {
                day++; chunk.a(1L);
                spawnedBolt = null; horse = null; additions = new ArrayList<>();
                Long seed = i == 0 ? Long.valueOf(17) : i == 1 ? Long.valueOf(spec[5]) : null;
                var row = step(0,seed,15,i == 1 ? CENTER : null,new int[0][]);
                row.keySet().retainAll(List.of("tick","seed","lcgBefore","lcgAfter","rngAfter"));
                row.put("inhabited",chunk.u()); row.put("difficulty",d_(strikeTarget).b()); row.put("added",additions);
                row.put("visualOnly", spawnedBolt != null && field(btm.class,"h").getBoolean(spawnedBolt));
                if (horse != null) {
                    var nbt = horse.f(new ub());
                    row.put("horsePos",new double[]{horse.dt(),horse.dv(),horse.dz()});
                    row.put("trap",nbt.q("SkeletonTrap")); row.put("trapTime",nbt.h("SkeletonTrapTime"));
                    row.put("age",nbt.h("Age")); row.put("health",horse.ew());
                    row.put("typeId",lt.f.a(horse.am()));
                    for (String key : List.of("UUID","Pos","Rotation")) nbt.r(key);
                    var bytes = new java.io.ByteArrayOutputStream();
                    var out = new java.io.DataOutputStream(bytes);
                    out.writeByte(10); nbt.a(out);
                    horseDefaults = java.util.HexFormat.of().formatHex(bytes.toByteArray());
                }
                ticks.add(row);
            }
            result.put("ticks",ticks); cases.add(result);
        }
        return cases;
    }

    public List<Map<String,Object>> lightningCases(ka registries) throws Exception {
        List<Map<String,Object>> cases = new ArrayList<>();
        long seed = 0;
        while (new dyz(seed).a(100000) != 0) seed++;
        for (boolean mobSpawning : List.of(false,true)) {
            reset();
            weatherRules = new dcs();
            weatherRules.a(dcs.e).a(mobSpawning, (net.minecraft.server.MinecraftServer)null);
            biome = registries.d(lu.aF).c(akr.a("minecraft:plains")).orElseThrow();
            rainy = true;
            // Isolate tickChunk's random stream from target search and entity ticking.
            // The live-server trace independently exercises redirection by real POIs.
            strikeTarget = CENTER.d();
            spawnedBolt = null;
            initial(CENTER,block("lightning_rod"));
            initial(CENTER.b(-1,0,0),block("grass_block"));
            var result = scene("storm_" + mobSpawning,false);
            result.put("mobSpawning",mobSpawning);
            // At this speed the trap roll shifts a 1/48 precipitation hit
            // into the sampled window, changing subsequent section positions.
            result.put("row",step(54,seed,15,CENTER,new int[0][]));
            if (!(spawnedBolt instanceof btm)) throw new IllegalStateException("missing lightning bolt");
            result.put("position",new double[]{spawnedBolt.dt(),spawnedBolt.dv(),spawnedBolt.dz()});
            cases.add(result);
            strikeTarget = null;
        }
        return cases;
    }
    @Override public boolean h(jd p) { return light == 15; }
    @Override public jm<ddw> t(jd p) { return biome; }
    @Override public int a(dyy.a type, int x, int z) {
        for (int y = 319; y >= -64; y--) if (type.e().test(a_(new jd(x,y,z)))) return y + 1;
        return -64;
    }
    private List<btn> strikeMobs;
    private List<Integer> struck;
    @Override public List<bsr> a(bsr except, ewx box, java.util.function.Predicate<? super bsr> test) {
        List<bsr> matches = new ArrayList<>();
        if (!(except instanceof btm) || strikeMobs == null) return matches;
        for (int i = 0; i < strikeMobs.size(); i++) {
            btn mob = strikeMobs.get(i);
            if (box.c(mob.cK()) && test.test(mob)) {
                matches.add(mob);
                // The larger query at removal is for the advancement only.
                if (box.d - box.a == 6) struck.add(i);
            }
        }
        return matches;
    }
    @Override public void a(bsr entity, brk source) {}
    @Override public void a(bsr entity, byte event) {}
    @Override public void a(cmx player, double x, double y, double z, jm<avo> sound, avq source,
                            float volume, float pitch, long seed) {}

    public List<Map<String,Object>> hitCases(ka registries) throws Exception {
        registry = registries;
        dimension = registries.d(lu.aK).a(akr.a("minecraft:overworld"));
        field(dcw.class,"J").set(this,new brl(registries));
        field(dcw.class,"f").set(this,new dyz(19));
        var cases = new ArrayList<Map<String,Object>>();
        var sharedFlag = bsr.class.getDeclaredMethod("i",int.class);
        sharedFlag.setAccessible(true);
        var collectEquipment = btn.class.getDeclaredMethod("H");
        collectEquipment.setAccessible(true);
        for (String name : List.of("ordinary","visual","move","rain","invulnerable","loaded_zero","armor","equipped")) {
            reset();
            field(dcw.class,"z").set(this,new dyz(95));
            weatherRules = new dcs();
            weatherRules.a(dcs.b).a(false,(net.minecraft.server.MinecraftServer)null);
            difficulty = bqo.c;
            rainy = name.equals("rain");
            strikeMobs = new ArrayList<>();
            var positions = new ArrayList<double[]>();
            double x=-32.5, y=70, z=40.5;
            double half=bsx.w.l()/2.0, height=bsx.w.m();
            for (double[] p : new double[][]{{x,y,z},{x+3+half,y,z},{x+3+half-.00001,y,z},
                    {x,y+9,z},{x,y+9-.00001,z},{x,y-3-height,z},{x,y-3-height+.00001,z},
                    {x,y,z-3-half},{x,y,z-3-half+.00001},{x,y,z+4}}) {
                btn cow = bsx.w.a(this);
                cow.e(p[0],p[1],p[2]);
                // Keep stationary targets alive through the second accepted strike.
                cow.f(buw.s).a(40.0);
                cow.v(40f);
                if (name.equals("armor")) {
                    cow.f(buw.a).a(12);
                    cow.f(buw.b).a(2);
                    cow.f(buw.r).a(2);
                    cow.C(2f);
                }
                if (name.equals("loaded_zero")) cow.h(0);
                if (name.equals("equipped")) {
                    cow.a(bsy.f,new cuq(cut.pG));
                    cow.a(bsy.e,new cuq(cut.pT));
                    collectEquipment.invoke(cow);
                }
                if (name.equals("invulnerable")) cow.n(true);
                strikeMobs.add(cow);
                positions.add(p);
            }
            long seed=6;
            btm bolt=bsx.am.a(this);
            var rng=new dyz(seed);
            field(bsr.class,"ah").set(bolt,rng);
            bolt.b=rng.g();
            field(btm.class,"g").setInt(bolt,1+rng.a(3));
            boolean visual=name.equals("visual");
            bolt.a(visual); bolt.e(x,y,z);
            var ticks=new ArrayList<Object>();
            for (time=1; time<=45; time++) {
                if (name.equals("move") && time==2) {
                    strikeMobs.get(0).e(x,y,z+6);
                    strikeMobs.get(9).e(x,y,z);
                }
                for (btn mob : strikeMobs) mob.aw();
                struck=new ArrayList<>();
                if (!bolt.dJ()) bolt.l();
                var row = new LinkedHashMap<String,Object>();
                row.put("struck",struck);
                var states = new ArrayList<double[]>();
                for (btn m : strikeMobs) {
                    states.add(new double[]{m.ew(),m.aC(),m.aO,m.am,m.fo(),(boolean)sharedFlag.invoke(m,0)?1:0});
                }
                row.put("mobs",states);
                ticks.add(row);
            }
            var scene=new LinkedHashMap<String,Object>();
            scene.put("name",name); scene.put("position",new double[]{x,y,z}); scene.put("seed",seed);
            scene.put("visual",visual); scene.put("positions",positions); scene.put("ticks",ticks);
            cases.add(scene);
        }
        strikeMobs=null;
        return cases;
    }
    @Override public boolean r(jd pos) { return strikeMobs != null ? rainy : super.r(pos); }
    public static Map<String,Object> livingTraits() {
        var traits = new TreeMap<String,Object>();
        for (bsx type : lt.f) {
            if (!bux.b(type)) continue;
            var attr = bux.a(type);
            traits.put(lt.f.b(type).toString(), Map.of("armor",attr.b(buw.a),"fireImmune",type.d()));
        }
        var equipment = new TreeMap<String,Object>();
        for (cul item : lt.g) {
            cuq stack = item.w();
            for (bsy slot : bsy.values()) {
                double[] values = {0,0};
                stack.a(slot,(attr,modifier) -> {
                    if (attr.equals(buw.a)) values[0] += modifier.c();
                    if (attr.equals(buw.b)) values[1] += modifier.c();
                });
                if (values[0]!=0 || values[1]!=0)
                    equipment.put(lt.g.b(item).toString(),Map.of("slot",slot.e(),"armor",values[0],"toughness",values[1]));
            }
        }
        return Map.of("mobs",traits,"equipment",equipment);
    }
    @Override public void a(jm<dxz> event, exc pos, dxz.a context) {}
    @Override public bnf ag() { return bnc.a; }
    @Override public int b(jd pos, int skyDarken) { return light; }
    @Override public epe b_(jd pos) { return a_(pos).u(); }
    @Override public dtc a_(jd pos) {
        dtc state = inside(pos) ? section(pos).a(pos.u() & 15, pos.v() & 15, pos.w() & 15)
                                : border.getOrDefault(pos,dga.a.o());
        if (copperProbes != null) {
            // Observe the original search, including rejected candidates, without drawing any RNG.
            var caller = CALLERS.walk(frames -> frames.skip(1).findFirst().orElseThrow());
            if (caller.getClassName().equals("btm")) {
                if (caller.getMethodName().equals("c") && caller.getDescriptor().equals("(Ldcw;Ljd;)Ljava/util/Optional;"))
                    copperProbes.add(cell(pos,state));
                if (caller.getMethodName().equals("b") && caller.getDescriptor().equals("(Ldcw;Ljd;)V")) {
                    if (copperReads.isEmpty()) {
                        try { copperSearchSeed = rawState((dyz)z) ^ 0x5deece66dL; }
                        catch (ReflectiveOperationException e) { throw new IllegalStateException(e); }
                    }
                    copperReads.add(cell(pos,state));
                }
            }
        }
        return state;
    }

    private static boolean inside(jd pos) {
        return (pos.u() >> 4) == CX && (pos.w() >> 4) == CZ && pos.v() >= -64 && pos.v() < 320;
    }

    private dvj section(jd pos) { return chunk.b(chunk.e(pos.v())); }

    private dtc put(jd pos, dtc state) {
        if (!inside(pos) && copperWrites != null) {
            dtc old = border.getOrDefault(pos,dga.a.o());
            border.put(pos.j(),state);
            return old;
        }
        if (!inside(pos)) throw new IllegalStateException("write outside chunk " + pos);
        return section(pos).a(pos.u() & 15, pos.v() & 15, pos.w() & 15, state);
    }

    @Override public boolean a(jd pos, dtc state, int flags) {
        dtc old = put(pos, state);
        if (old == state) return false;
        if (copperWrites != null) copperWrites.add(cell(pos,state));
        if (captureFireWrites || copperWrites != null) tracked.add(pos.j());
        else if (!tracked.contains(pos)) throw new IllegalStateException("untracked block change " + pos);
        if (old.b() != state.b()) state.a(this, pos, old, false);
        // Run the original neighbor shape callbacks after onPlace.
        state.a(this, pos, flags & ~33);
        return true;
    }

    @Override public void a(jd pos, dfy block, int delay) {
        blockTicks.a(new eyt<>(block, pos.j(), time + delay, order++));
    }

    @Override public void a(jd pos, epd fluid, int delay) {
        fluidTicks.a(new eyt<>(fluid, pos.j(), time + delay, order++));
    }

    @Override public jd a(int x, int y, int z, int mask) {
        jd result = super.a(x, y, z, mask);
        samples.add(coords(result));
        return result;
    }

    @Override public void a(jd pos) {
        // tickPrecipitation is the only non-section caller of getBlockRandomPos
        // in clear weather. The output records its draw separately.
        precipitation.add(samples.removeLast());
    }

    private static int[] coords(jd pos) { return new int[]{pos.u(), pos.v(), pos.w()}; }

    private static int[] cell(jd pos, dtc state) {
        return new int[]{pos.u(), pos.v(), pos.w(), dfy.i(state)};
    }

    private static dtc block(String name) { return lt.e.a(akr.a("minecraft:" + name)).o(); }

    private void initial(jd pos, dtc state) {
        if (!tracked.add(pos)) throw new IllegalStateException("duplicate position");
        if (inside(pos)) put(pos, state); else border.put(pos.j(),state);
    }

    // Arrange an input at which vanilla's next section draw lands on target.
    private static int aim(jd target) {
        int bits = (target.u() & 15) | (target.w() & 15) << 8 | (target.v() & 15) << 16;
        return ((bits << 2) - 1013904223) * 0xaaaaaaab;
    }

    private static long rawState(dyz runtime) throws ReflectiveOperationException {
        return ((AtomicLong) field(dyz.class, "h").get(runtime)).get();
    }

    private Map<String,Object> step(int speed, Long seed, int brightness, jd target, int[][] edits) throws Exception {
        time++;
        light = brightness;
        if (seed != null) field(dcw.class, "z").set(this, new dyz(seed));
        dyz runtime = (dyz) z;
        long inputSeed = rawState(runtime) ^ 0x5deece66dL;
        if (target != null) t = aim(target);
        int before = t;
        samples = new ArrayList<>();
        precipitation = new ArrayList<>();
        for (int[] edit : edits) a(new jd(edit[0], edit[1], edit[2]), dfy.a(edit[3]), 3);
        blockTicks.a(time, 65536, (pos, block) -> {
            dtc state = a_(pos);
            if (state.a(block)) state.a(this, pos, runtime);
        });
        super.a(chunk, speed);
        long after = rawState(runtime);
        Map<String,Object> row = new LinkedHashMap<>();
        row.put("tick", time);
        row.put("speed", speed);
        row.put("seed", inputSeed);
        row.put("lcgBefore", before);
        row.put("lcgAfter", t);
        row.put("rngAfter", after);
        row.put("light", light);
        row.put("edits", edits);
        row.put("samples", samples);
        row.put("precipitation", precipitation);
        row.put("states", tracked.stream().mapToInt(pos -> dfy.i(a_(pos))).toArray());
        return row;
    }

    private Map<String,Object> scene(String name, boolean recount) {
        if (recount) for (dvj section : chunk.d()) section.g();
        Map<String,Object> result = new LinkedHashMap<>();
        result.put("name", name);
        result.put("chunk", new int[]{CX, CZ});
        result.put("recount", recount);
        result.put("initial", tracked.stream().map(pos -> cell(pos, a_(pos))).toList());
        return result;
    }

    public List<Map<String,Object>> capture(ka registries) throws Exception {
        biomes = registries.d(lu.aF);
        List<Map<String,Object>> cases = new ArrayList<>();
        dtc leaf = block("oak_leaves");
        dtc log = block("oak_log");
        // Select the vanilla empty loot table. The drops call, decay test and
        // removeBlock remain intact and cannot consume an independently seeded loot stream.
        field(dtb.class, "aQ").set(leaf.b(), ern.a);
        int[][] none = new int[0][];
        reset();
        jd trunk = CENTER.b(-1, 0, 0);
        jd tip = CENTER.b(1, 0, 0);
        initial(trunk, log);
        initial(CENTER, leaf.a(dki.d, 1));
        initial(tip, leaf.a(dki.d, 2));
        initial(CENTER.b(4, 0, 4), leaf.a(dki.e, true));
        initial(CENTER.b(-4, 0, -4), log);
        initial(CENTER.b(-3, 0, -4), leaf.a(dki.d, 1));
        var tree = scene("oak_log_removed", false);
        List<Map<String,Object>> ticks = new ArrayList<>();
        for (int i = 1; i <= 13; i++) {
            jd target = i == 9 ? CENTER : i == 10 ? tip : null;
            ticks.add(step(i <= 2 ? 0 : 3, 13L, 15, target,
                i == 1 ? new int[][]{cell(trunk, dga.a.o())} : none));
        }
        tree.put("ticks", ticks);
        cases.add(tree);

        for (String layout : List.of("hydrated", "dry", "row", "cross", "diagonal")) {
            reset();
            dtc wheat = block("wheat");
            initial(CENTER, wheat);
            int moisture = layout.equals("dry") ? 0 : 7;
            for (int x = -1; x <= 1; x++) for (int z = -1; z <= 1; z++) {
                initial(CENTER.b(x, -1, z), block("farmland").a(div.b, moisture));
            }
            if (layout.equals("row") || layout.equals("cross")) initial(CENTER.b(1, 0, 0), wheat);
            if (layout.equals("cross")) initial(CENTER.b(0, 0, 1), wheat);
            if (layout.equals("diagonal")) initial(CENTER.b(1, 0, 1), wheat);
            var crop = scene("wheat_" + layout, false);
            ticks = new ArrayList<>();
            for (int i = 0; i < 150; i++) {
                int brightness = i == 1 ? 8 : i % 2 == 0 ? 9 : 15;
                ticks.add(step(i == 0 ? 0 : 3, 193L + i, brightness, CENTER, none));
                if (i > 1 && a_(CENTER).c(dht.f) == 7) {
                    ticks.add(step(3, 13L, 15, CENTER, none));
                    break;
                }
            }
            if (a_(CENTER).c(dht.f) != 7) throw new IllegalStateException("wheat did not mature " + layout);
            crop.put("ticks", ticks);
            cases.add(crop);
        }

        for (boolean recount : new boolean[]{false, true}) {
            reset();
            initial(CENTER.b(0, -96, 0), block("water"));
            initial(CENTER, leaf);
            initial(CENTER.b(0, 32, 0), block("stone"));
            initial(CENTER.b(0, 48, 0), block("wheat"));
            var scanning = scene("section_order_" + (recount ? "recounted" : "live_edits"), recount);
            ticks = new ArrayList<>();
            for (int i = 0; i < 12; i++) {
                ticks.add(step(i == 0 ? 0 : i == 1 ? 1 : i == 2 ? 6 : 3, (long) i, 8, null, none));
            }
            scanning.put("ticks", ticks);
            cases.add(scanning);
        }

        reset();
        jd naturalLeaf = CENTER.b(-6, -3, 1);
        jd naturalTrunk = naturalLeaf.b(-1, 0, 0);
        jd naturalWheat = CENTER.b(-1, 6, -7);
        initial(naturalTrunk, log);
        initial(naturalLeaf, leaf.a(dki.d, 1));
        initial(naturalWheat, block("wheat").a(dht.f, 6));
        for (int x = -1; x <= 1; x++) for (int z = -1; z <= 1; z++) {
            initial(naturalWheat.b(x, -1, z), block("farmland").a(div.b, 7));
        }
        var continuous = scene("continuous_clear_chunk", false);
        continuous.put("continuous", true);
        ticks = new ArrayList<>();
        for (int i = 1; i <= 16; i++) {
            ticks.add(step(3, i == 1 ? 4L : null, 15, null,
                i == 1 ? new int[][]{cell(naturalTrunk, dga.a.o())} : none));
        }
        if (!a_(naturalLeaf).i() || a_(naturalWheat).c(dht.f) != 7) {
            throw new IllegalStateException("continuous scene lost its decay or growth event");
        }
        continuous.put("ticks", ticks);
        cases.add(continuous);

        reset();
        jd wetLeaf = new jd(CX * 16 + 4, 73, CZ * 16 + 2);
        jd wetPersistent = wetLeaf.e();
        dtc wet = leaf.a(dki.f, true);
        dtc persistent = wet.a(dki.e, true);
        initial(wetLeaf, wet);
        initial(wetPersistent, persistent);
        var waterlogged = scene("waterlogged_leaf_decay", false);
        // The first two samples visit the adjacent natural and persistent leaf.
        t = 1197798;
        waterlogged.put("ticks", List.of(step(3, 13L, 15, null, none)));
        if (a_(wetLeaf) != block("water") || a_(wetPersistent) != persistent
                || !fluidTicks.a(wetLeaf, epf.c) || !fluidTicks.a(wetPersistent, epf.c)) {
            throw new IllegalStateException("unexpected waterlogged decay result or pending water ticks");
        }
        cases.add(waterlogged);
        for (String layout : List.of("dry_bare", "dry_wheat", "water_corner", "water_below", "water_five",
                "waterlogged_above", "flowing_border", "rain_plains", "rain_glass", "rain_desert", "rain_snowy_plains",
                "covered", "cover_removed", "fence_gate")) {
            cases.add(farmland(layout));
        }
        cases.add(soilBeforeWheat(true));
        cases.add(soilBeforeWheat(false));
        return cases;
    }

    @Override public boolean a(jd pos, dtc state, int flags, int recursion) {
        return a(pos,state,flags);
    }

    private dcs weatherRules;
    @Override public dcs ab() { return weatherRules != null ? weatherRules : super.ab(); }
    @Override public int I_() { return -64; }
    @Override public int am() { return 320; }
    @Override public int a(ddf layer, jd pos) { return light; }

    // Invoke the original tickPrecipitation for one selected column at a time.
    public List<Map<String,Object>> precipitationCases(ka registries) throws Exception {
        biomes = registries.d(lu.aF);
        List<Map<String,Object>> cases = new ArrayList<>();
        for (String name : List.of("snow_one", "snow_accumulate", "snow_disabled", "too_bright",
                "warm", "clear_bank", "lake_interior", "flowing_water", "waterlogged",
                "ice_support", "honey_support", "grass_snowy", "roof")) {
            reset();
            weatherRules = new dcs();
            int cap = name.equals("snow_accumulate") ? 12 : name.equals("snow_disabled") ? 0 : 1;
            weatherRules.a(dcs.T).a(cap, null);
            rainy = !name.equals("clear_bank");
            light = name.equals("too_bright") ? 10 : 9;
            String climate = name.equals("warm") ? "plains" : "snowy_plains";
            biome = biomes.c(akr.a("minecraft:" + climate)).orElseThrow();
            dtc ground = switch (name) {
                case "clear_bank", "lake_interior" -> block("water");
                case "flowing_water" -> block("water").a(dko.b, 1);
                case "waterlogged" -> block("oak_leaves").a(dki.f,true).a(dki.e,true);
                case "ice_support" -> block("ice");
                case "honey_support" -> block("honey_block");
                case "grass_snowy" -> block("grass_block");
                default -> block("stone");
            };
            initial(CENTER, ground);
            initial(CENTER.d(), block("air"));
            initial(CENTER.b(0,2,0), block("air"));
            if (name.equals("lake_interior")) {
                for (jd p : List.of(CENTER.h(), CENTER.i(), CENTER.f(), CENTER.g())) initial(p,block("water"));
            }
            if (name.equals("roof")) {
                initial(CENTER.b(0,3,0),block("glass"));
                initial(CENTER.b(0,4,0),block("air"));
            }
            var result = scene(name,false);
            result.put("rain",rainy);
            result.put("biome",climate);
            result.put("light",light);
            result.put("cap",cap);
            List<int[]> states = new ArrayList<>();
            for (int i=0; i<(cap>1 ? 10 : 2); i++) {
                super.a(CENTER);
                states.add(tracked.stream().mapToInt(p -> dfy.i(a_(p))).toArray());
            }
            result.put("states",states);
            cases.add(result);
        }
        return cases;
    }

    private Map<String,Object> farmland(String layout) throws Exception {
        reset();
        jd soil = layout.equals("flowing_border") ? CENTER.b(7, 0, 0) : CENTER;
        boolean drying = layout.startsWith("dry_");
        initial(soil, block("farmland").a(div.b, drying ? 2 : 1));
        switch (layout) {
            case "dry_wheat" -> initial(soil.d(), block("wheat"));
            case "water_corner" -> initial(soil.b(-4, 0, 4), block("water"));
            case "water_below" -> initial(soil.e(), block("water"));
            case "water_five" -> initial(soil.b(5, 0, 0), block("water"));
            case "waterlogged_above" -> initial(soil.b(-4, 1, 4), block("oak_leaves").a(dki.f, true).a(dki.e, true));
            case "flowing_border" -> initial(soil.b(4, 0, 0), block("water").a(dko.b, 3));
            case "rain_glass" -> initial(soil.b(0, 3, 0), block("glass"));
            case "covered", "cover_removed", "fence_gate" -> initial(soil.d(), block("air"));
        }
        var result = scene("farmland_" + layout, false);
        rainy = layout.startsWith("rain_");
        if (rainy) {
            String climate = switch (layout) {
                case "rain_desert" -> "desert";
                case "rain_snowy_plains" -> "snowy_plains";
                default -> "plains";
            };
            biome = biomes.c(akr.a("minecraft:" + climate)).orElseThrow();
            result.put("rain", true);
            result.put("biome", climate);
        }
        boolean cover = layout.equals("covered") || layout.equals("cover_removed") || layout.equals("fence_gate");
        List<Map<String,Object>> ticks = new ArrayList<>();
        for (int i = 0; i < (drying ? 4 : 2); i++) {
            int[][] changes = new int[0][];
            if (cover && i == 0) {
                changes = new int[][]{cell(soil.d(), block(layout.equals("fence_gate") ? "oak_fence_gate" : "stone"))};
            }
            if (layout.equals("cover_removed") && i == 1) changes = new int[][]{cell(soil.d(), block("air"))};
            var row = step(cover ? 0 : 1, 13L, 15, soil, changes);
            if (cover) row.put("pendingFarmland", blockTicks.a(soil, block("farmland").b()));
            ticks.add(row);
        }
        result.put("ticks", ticks);
        return result;
    }

    private Map<String,Object> soilBeforeWheat(boolean irrigated) throws Exception {
        reset();
        jd soil = new jd(CX * 16 + 3, 66, CZ * 16 + 2);
        initial(soil, block("farmland"));
        initial(soil.d(), block("wheat"));
        if (irrigated) initial(soil.b(4, 0, 4), block("water"));
        var result = scene(irrigated ? "farmland_before_wheat" : "farmland_before_wheat_dry", false);
        // The two native samples select the soil then its crop. At seed 70 the
        // crop passes nextInt(7) on wet soil and fails nextInt(13) on dry soil.
        t = 586128;
        result.put("ticks", List.of(step(2, 70L, 15, null, new int[0][])));
        if (a_(soil).c(div.b) != (irrigated ? 7 : 0) || a_(soil.d()).c(dht.f) != (irrigated ? 1 : 0)) {
            throw new IllegalStateException("unexpected soil/crop order");
        }
        return result;
    }
}
