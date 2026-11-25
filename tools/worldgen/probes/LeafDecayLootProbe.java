package atlasprobe;

import com.google.gson.Gson;
import com.google.gson.JsonParser;
import com.mojang.serialization.Codec;
import com.mojang.serialization.DynamicOps;
import com.mojang.serialization.JsonOps;
import java.io.InputStreamReader;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.TreeMap;
import java.util.function.Consumer;

import static atlasprobe.IglooProbeSupport.cls;

/** Executes the shipped tables on the original named random sequences. */
public final class LeafDecayLootProbe {
    public static void main(String[] args) throws Exception {
        long seed = Long.parseLong(args[1]);
        int rolls = Integer.parseInt(args[2]);
        Object version = cls("t").getMethod("a").invoke(null);
        cls("ab").getMethod("a", cls("ae")).invoke(null, version);
        cls("akt").getMethod("a").invoke(null);
        Object access = IglooProbeSupport.registryAccess();
        var ops = (DynamicOps<?>)cls("akp").getMethod("a", DynamicOps.class, cls("jo$a"))
                .invoke(null, JsonOps.INSTANCE, access);
        var codec = (Codec<?>)cls("erw").getField("d").get(null);
        var sequenceId = cls("erw").getDeclaredField("h");
        sequenceId.setAccessible(true);
        Object sequences = cls("bqy").getConstructor(long.class).newInstance(seed);
        var getSequence = cls("bqy").getMethod("a", cls("akr"));
        var context = cls("err").getDeclaredConstructor(cls("eru"), cls("ayw"), cls("jn$a"));
        context.setAccessible(true);
        var getRawDrops = cls("erw").getMethod("a", cls("err"), Consumer.class);
        var params = cls("eru").getConstructor(cls("aqu"), Map.class, Map.class, float.class);
        var itemRegistry = cls("lt").getField("g").get(null);
        var itemKey = cls("jz").getMethod("b", Object.class);
        var stackItem = cls("cuq").getMethod("g");
        var stackCount = cls("cuq").getMethod("H");
        var origin = cls("exc").getConstructor(double.class, double.class, double.class)
                .newInstance(0.5, 64.5, 0.5);
        var tool = cls("cuq").getField("l").get(null);
        var tables = new TreeMap<String, Map<Integer, List<String>>>();
        for (int i = 3; i < args.length; i++) {
            String leaf = args[i];
            String resource = "/data/minecraft/loot_table/blocks/" + leaf + ".json";
            Object table;
            try (var reader = new InputStreamReader(cls("erw").getResourceAsStream(resource), StandardCharsets.UTF_8)) {
                table = codec.parse((DynamicOps)ops, JsonParser.parseReader(reader)).getOrThrow();
            }
            Object key = ((Optional<?>)sequenceId.get(table)).orElseThrow();
            Object random = getSequence.invoke(sequences, key);
            Object state = RuinedPortalProbeSupport.block(leaf);
            Object inputs = params.newInstance(null, Map.of(
                    cls("eun").getField("f").get(null), origin,
                    cls("eun").getField("g").get(null), state,
                    cls("eun").getField("i").get(null), tool), Map.of(), 0f);
            Object lootContext = context.newInstance(inputs, random, null);
            var nonempty = new TreeMap<Integer, List<String>>();
            for (int roll = 0; roll < rolls; roll++) {
                var stacks = new ArrayList<Object>();
                getRawDrops.invoke(table, lootContext, (Consumer<Object>)stacks::add);
                if (stacks.isEmpty()) continue;
                var drops = new ArrayList<String>();
                for (Object stack : stacks)
                    drops.add(itemKey.invoke(itemRegistry, stackItem.invoke(stack)) + "*" + stackCount.invoke(stack));
                nonempty.put(roll, drops);
            }
            tables.put("minecraft:" + leaf, nonempty);
        }
        Files.writeString(Path.of(args[0]), new Gson().toJson(tables), StandardCharsets.UTF_8);
    }
}
