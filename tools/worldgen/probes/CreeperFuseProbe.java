import com.mojang.authlib.GameProfile;
import java.lang.reflect.Field;
import java.util.*;

/** Keep the two original goal selectors, Creeper.tick and explodeCreeper; hold movement fixed. */
public final class CreeperFuseProbe {
    static final class Target extends cmx {
        Target(RandomTickLevel level) {
            super(level,new jd(0,70,0),0,new GameProfile(new UUID(0,1),"Target"));
        }
        public boolean R_() { return false; }
        public boolean f() { return false; }
        public eyb ck() { return null; }
    }
    static final class EndBaseTick extends RuntimeException {}
    static final class Creeper extends cjp {
        boolean visible=true, primed;
        Creeper(RandomTickLevel world) throws ReflectiveOperationException {
            super(bsx.x,world);
            var profiler=can.class.getDeclaredField("d");
            profiler.setAccessible(true);
            java.util.function.Supplier<bnf> supplier=world::ag;
            profiler.set(bW,supplier);
            profiler.set(bX,supplier);
        }
        @Override protected void B() {
            bX.a(1,new ccf<>(this,cmx.class,true));
            bW.a(2,new cbt(this));
        }
        @Override public eyb ck() { return null; }
        @Override public boolean F(bsr other) { return visible; }
        @Override public void a(avo sound,float volume,float pitch) {
            if (sound==avp.gk) primed=true;
        }
        @Override public void aw() {
            if (!dJ() && !fZ()) fm();
            throw new EndBaseTick();
        }
    }
    static List<Map<String,Object>> capture(RandomTickLevel level) throws Exception {
        var result=new ArrayList<Map<String,Object>>();
        Field swell=cjp.class.getDeclaredField("cc"), random=bsr.class.getDeclaredField("ah");
        swell.setAccessible(true);
        random.setAccessible(true);
        for (String name:List.of("approach","retreat","occluded","charged","delayed","ignited","ignited_ai","custom")) {
            var player=new Target(level);
            level.fusePlayer=player;
            var mob=new Creeper(level);
            mob.e(77);
            mob.e(0,70,0);
            var nbt=mob.f(new ub());
            boolean charged=name.equals("charged") || name.equals("custom");
            boolean ignited=name.startsWith("ignited") || name.equals("custom");
            boolean noAI=ignited && !name.equals("ignited_ai");
            nbt.a("powered",charged);
            nbt.a("ignited",ignited);
            nbt.a("NoAI",noAI);
            if (name.equals("custom")) {
                nbt.a("Fuse",(short)4);
                nbt.a("ExplosionRadius",(byte)5);
            }
            mob.g(nbt);
            mob.ar().b();
            long seed=name.equals("delayed") ? 3 : 0;
            random.set(mob,new dyz(seed));
            level.explosionPower=null;
            var ticks=new ArrayList<Object>();
            for (int t=1;t<=80;t++) {
                double x=ignited ? 20 : 2.99;
                if (name.equals("approach") || name.equals("retreat")) {
                    if (t<=4) x=3;
                    if (t>=10 && t<=13) x=7;
                    if (name.equals("retreat") && ((t>=14 && t<=19) || (t>=24 && t<=42))) x=7.001;
                }
                mob.visible=!name.equals("occluded") || t<9 || t>14;
                player.e(x,70,0);
                mob.ai=t;
                mob.primed=false;
                try { mob.l(); } catch (EndBaseTick end) {}
                var row=new LinkedHashMap<String,Object>();
                row.put("distance",x);
                row.put("visible",mob.visible);
                row.put("direction",mob.t());
                var dirty=mob.ar().b();
                row.put("directionDirty",dirty!=null && dirty.stream().anyMatch(v -> v.a()==16));
                row.put("swell",swell.getInt(mob));
                row.put("primed",mob.primed);
                if (level.explosionPower!=null) row.put("power",level.explosionPower);
                ticks.add(row);
                if (level.explosionPower!=null) break;
            }
            if (level.explosionPower==null) throw new IllegalStateException(name+" did not explode");
            var scene=new LinkedHashMap<String,Object>();
            scene.put("name",name);
            scene.put("seed",seed);
            scene.put("charged",charged);
            scene.put("ignited",ignited);
            scene.put("noAI",noAI);
            scene.put("ticks",ticks);
            result.add(scene);
        }
        level.fusePlayer=null;
        return result;
    }
}
