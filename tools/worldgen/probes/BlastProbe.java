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
        server.a(false);
    }
}
