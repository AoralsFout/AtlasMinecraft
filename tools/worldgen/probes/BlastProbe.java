import com.google.gson.Gson;
import java.util.*;
import java.io.*;
import java.nio.file.*;
import java.security.MessageDigest;
import net.minecraft.server.MinecraftServer;
import atlasprobe.BlastProbeAgent;
import io.netty.buffer.Unpooled;
import io.netty.buffer.ByteBufUtil;

public final class BlastProbe {
    private static List<Object> collectItems(aqu level) {
        // Removing entities during iteration can skip the next registered item.
        var spawned = new ArrayList<cjh>();
        for (bsr entity : level.A()) {
            if (entity instanceof cjh item) spawned.add(item);
        }
        var rows = new ArrayList<Object>();
        for (cjh item : spawned) {
            cuq stack = item.p();
            rows.add(Map.of("item", lt.g.b(stack.g()).toString(), "count", stack.H(),
                    "pos", List.of(item.dt(), item.dv(), item.dz())));
            item.aq();
        }
        return rows;
    }

    private static Map<String,String> itemPackets(aqu level) {
        var drop = new cjh(level, -3.25, 200.125, 17.875, new cuq(lt.g.a(akr.b("diamond")), 19),
                -0.05, 0.2, 0.075);
        drop.e(77);
        drop.a_(new UUID(1, 2));
        drop.t(45);
        var tracker = new aqt(level, drop, 20, true, ignored -> {});
        var bytes = Unpooled.buffer();
        try {
            var buf = new wk(bytes, level.H_());
            abv.a.encode(buf, (abv)drop.a(tracker));
            String spawn = ByteBufUtil.hexDump(bytes);
            bytes.clear();
            aez.a.encode(buf, new aez(77, drop.ar().c()));
            return Map.of("spawn",spawn,"metadata",ByteBufUtil.hexDump(bytes));
        } finally {
            bytes.release();
        }
    }

    private static List<Double> position(bsr e) {
        return List.of(e.dt(), e.dv(), e.dz());
    }

    private static List<Double> vector(exc v) {
        return List.of(v.c, v.d, v.e);
    }

    private static Map<String,String> tntPackets(aqu level) {
        var tnt = new cji(bsx.bc, level);
        tnt.e(77);
        tnt.a_(new UUID(1, 2));
        tnt.a_(8.5, 200, 8.5);
        tnt.n(-0.01, (double)0.2f, 0.015);
        tnt.b(18);
        var tracker = new aqt(level, tnt, 10, true, ignored -> {});
        var bytes = Unpooled.buffer();
        try {
            var buf = new wk(bytes, level.H_());
            abv.a.encode(buf, (abv)tnt.a(tracker));
            String spawn = ByteBufUtil.hexDump(bytes);
            bytes.clear();
            aez.a.encode(buf, new aez(77, tnt.ar().c()));
            return Map.of("spawn",spawn,"metadata",ByteBufUtil.hexDump(bytes));
        } finally {
            bytes.release();
        }
    }

    private static void captureTntChains(MinecraftServer server, aqu level, Gson gson) throws Exception {
        var tickListField = aqu.class.getDeclaredField("M");
        tickListField.setAccessible(true);
        var tickList = (dxm)tickListField.get(level);
        var tickEntity = aqu.class.getDeclaredMethod("a", brc.class, bnf.class, bsr.class);
        tickEntity.setAccessible(true);
        var tickRate = level.s();
        var profiler = level.ag();
        var fuseField = cjp.class.getDeclaredField("cd");
        fuseField.setAccessible(true);
        var itemHealth = cjh.class.getDeclaredField("k");
        itemHealth.setAccessible(true);
        level.ab().a(dcs.c).a(true, server);
        level.ab().a(dcs.R).a(false, server);
        level.ab().a(dcs.h).a(true, server);
        var scenarios = new ArrayList<Object>();
        for (long seed : new long[]{17, 9231, 2026}) {
            boolean platform = seed != 17;
            boolean cascade = seed == 2026;
            boolean decay = seed != 9231;
            level.ab().a(dcs.S).a(decay, server);
            var blocks = new ArrayList<int[]>();
            for (int x=4; x<=14; x++) for (int z=5; z<=11; z++) {
                if (!platform || x <= 8) blocks.add(new int[]{x,199,z,dfy.i(dga.F.o())});
            }
            blocks.add(new int[]{8,200,8,dfy.i(dga.ck.o())});
            blocks.add(new int[]{9,200,8,dfy.i(lt.e.a(akr.b("dirt")).o())});
            if (cascade) blocks.add(new int[]{12,200,8,dfy.i(dga.ck.o())});
            for (int[] p : blocks) level.a(new jd(p[0],p[1],p[2]),dfy.a(p[3]),2);
            var creeper = bsx.x.a(level);
            creeper.e(7.5,200,8.5);
            creeper.f(true);
            creeper.u(true);
            creeper.gk();
            fuseField.setInt(creeper,1);
            if (!level.b(creeper)) throw new AssertionError("creeper was not added");
            level.z.b(seed);
            var ticks = new ArrayList<Object>();
            var firstDrops = new ArrayList<cjh>();
            int explosions = 0;
            for (int tick=0; tick<=80; tick++) {
                BlastProbeAgent.lastExplosion = null;
                // Run the ServerLevel.tick entity phase with its original callback and gates.
                tickList.a(entity -> {
                    try {
                        tickEntity.invoke(level,tickRate,profiler,entity);
                    } catch (ReflectiveOperationException error) {
                        throw new IllegalStateException("entity tick failed",error);
                    }
                });
                var row = new TreeMap<String,Object>();
                var blast = (dco)BlastProbeAgent.lastExplosion;
                if (blast != null) {
                    explosions++;
                    row.put("explosion", Map.of("power",blast.a(),"center",vector(blast.b()),"interaction",blast.j().ordinal()));
                }
                var tnt = new ArrayList<Object>();
                for (bsr entity : level.A()) {
                    if (entity instanceof cji primed)
                        tnt.add(Map.of("fuse",primed.t(),"pos",position(primed),"motion",vector(primed.dr())));
                    if (tick == 0 && entity instanceof cjh item) firstDrops.add(item);
                }
                var items = new ArrayList<Object>();
                for (var item : firstDrops) items.add(Map.of("alive",!item.dJ(),"health",itemHealth.getInt(item),
                        "pos",position(item),"motion",vector(item.dr())));
                row.put("tnt",tnt);
                row.put("items",items);
                row.put("tick",tick);
                ticks.add(row);
                if (explosions >= (cascade ? 3 : 2) && tnt.isEmpty()) break;
            }
            if (firstDrops.size() != 1 || explosions < 2) throw new AssertionError("incomplete TNT scenario for seed "+seed);
            scenarios.add(Map.of("seed",seed,"decay",decay,"blocks",blocks,"ticks",ticks));
            var remaining = new ArrayList<bsr>();
            for (bsr entity : level.A()) if (entity instanceof cji || entity instanceof cjh) remaining.add(entity);
            remaining.forEach(bsr::aq);
            for (int[] p : blocks) level.a(new jd(p[0],p[1],p[2]),dga.a.o(),2);
        }
        Files.writeString(Path.of("tnt-chain.json"),gson.toJson(Map.of("scenarios",scenarios,"packets",tntPackets(level))));
    }

    public static void capture(Object instance) throws Exception {
        MinecraftServer server = (MinecraftServer)instance;
        aqu level = server.I();
        var gson = new Gson();
        var resistance = new ArrayList<List<Number>>();
        float last = Float.NaN;
        for (int id=0; id<dfy.q.c(); id++) {
            dtc state=dfy.a(id);
            float r=state.i() && state.u().c() ? -1 : Math.max(state.b().e(),state.u().i());
            if (r!=last) resistance.add(List.of(id,r));
            last=r;
        }
        Files.writeString(Path.of("resistance.json"),gson.toJson(resistance));
        var aliases=new TreeMap<String,String>();
        for(dfy block:lt.e) {
            String name=lt.e.b(block).a();
            String table=block.v().a().a();
            if (!table.equals("blocks/"+name)) aliases.put(name,table);
        }
        Files.writeString(Path.of("aliases.json"),gson.toJson(aliases));
        String[] palette={"dirt","glass","oak_planks","stone","obsidian","white_wool","clay","water"};
        var initial=new ArrayList<int[]>();
        for (int x=3;x<=13;x++) for(int z=3;z<=13;z++) {
            int index=Math.floorMod(x*3+z*5+(x*z)%3,palette.length);
            int id=dfy.i(lt.e.a(akr.b(palette[index])).o());
            initial.add(new int[]{x,199,z,id});
            if ((x+z)%3==0 && (x!=8 || z!=8))
                initial.add(new int[]{x,200,z,dfy.i(lt.e.a(akr.b("dirt")).o())});
        }
        var rows=new ArrayList<Object>();
        for(long seed:new long[]{17,9231}) for(int power:new int[]{3,6}) for(int mode=0;mode<4;mode++) {
            level.ab().a(dcs.c).a(mode!=0,server);
            level.ab().a(dcs.R).a(mode!=1,server);
            level.ab().a(dcs.h).a(mode!=3,server);
            level.L().a();
            for(int[] v:initial) level.a(new jd(v[0],v[1],v[2]),dfy.a(v[3]),2);
            var creeper=bsx.x.a(level);
            creeper.e(8.5,200,8.5);
            if(power==6) creeper.a(level,bsx.am.a(level));
            var detonate=cjp.class.getDeclaredMethod("gn");
            detonate.setAccessible(true);
            level.z.b(seed);
            BlastProbeAgent.lastExplosion=null;
            detonate.invoke(creeper);
            dco explosion=(dco)BlastProbeAgent.lastExplosion;
            if(explosion.a()!=power) throw new AssertionError("unexpected creeper power");
            var bytes=new ByteArrayOutputStream();
            var data=new DataOutputStream(bytes);
            for(jd p:explosion.i()) { data.writeInt(p.u()); data.writeInt(p.v()); data.writeInt(p.w()); }
            var buffer=Unpooled.buffer();
            acz.a.encode(new wk(buffer,level.H_()),new acz(8.5,200,8.5,power,explosion.i(),null,
                explosion.j(),explosion.k(),explosion.l(),explosion.m()));
            byte[] packet=new byte[buffer.readableBytes()];
            buffer.readBytes(packet);
            buffer.release();
            var destroyed=new ArrayList<Integer>();
            for(int i=0;i<initial.size();i++) {
                int[] v=initial.get(i);
                if(level.a_(new jd(v[0],v[1],v[2])).i()) destroyed.add(i);
            }
            var items=collectItems(level);
            rows.add(Map.of("seed",seed,"power",power,"mode",mode,"interaction",explosion.j().ordinal(),
                "affected",explosion.i().size(),"hash",HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest(bytes.toByteArray())),
                "destroyed",destroyed,"items",items,"next",level.z.g(),"packet",HexFormat.of().formatHex(packet)));
        }
        for(int[] v:initial) level.a(new jd(v[0],v[1],v[2]),dga.a.o(),2);
        var containers=new ArrayList<Object>();
        for(boolean drops:new boolean[]{true,false}) {
            level.ab().a(dcs.h).a(drops,server);
            level.ab().a(dcs.R).a(false,server);
            var p=new jd(8,200,8);
            level.a(p,lt.e.a(akr.b("chest")).o(),2);
            var chest=(bqk)level.c_(p);
            chest.a(1,new cuq(lt.g.a(akr.b("diamond")),40));
            chest.a(7,new cuq(lt.g.a(akr.b("apple")),2));
            level.z.b(17L);
            level.a(bsx.x.a(level),8.5,200,8.5,3,dcw.a.c);
            var items=collectItems(level);
            containers.add(Map.of("drops",drops,"items",items,"next",level.z.g()));
        }
        Files.writeString(Path.of("blast.json"),gson.toJson(Map.of("initial",initial,"cases",rows,
                "containers",containers,"itemPackets",itemPackets(level))));
        captureTntChains(server,level,gson);
        server.a(false);
    }
}
