package atlasprobe;

import com.google.gson.Gson;
import java.io.*;
import java.lang.reflect.Field;
import java.util.List;
import sun.misc.Unsafe;

/** The server JAR seals the default package. Define the isolated level in a child loader. */
public final class RandomTickProbe {
    private static final class Loader extends ClassLoader {
        Class<?> loadLevel() throws IOException {
            try (InputStream input = getResourceAsStream("RandomTickLevel.class")) {
                byte[] bytes = input.readAllBytes();
                return defineClass("RandomTickLevel", bytes, 0, bytes.length);
            }
        }
    }

    public static void main(String[] args) throws Exception {
        Object version = Class.forName("t").getMethod("a").invoke(null);
        Class.forName("ab").getMethod("a", Class.forName("ae")).invoke(null, version);
        Class.forName("akt").getMethod("a").invoke(null);
        RuinedPortalProbeSupport.bindTags();
        Class<?> type = new Loader().loadLevel();
        Field field = Unsafe.class.getDeclaredField("theUnsafe");
        field.setAccessible(true);
        Object world = ((Unsafe) field.get(null)).allocateInstance(type);
        Gson gson = new Gson();
        // Bootstrapping redirects standard output through the logger.
        var out = new PrintStream(new BufferedOutputStream(new FileOutputStream(FileDescriptor.out)));
        out.println("sizes " + gson.toJson(type.getMethod("entitySizes").invoke(null)));
        out.println("flags " + gson.toJson(type.getMethod("stateFlags").invoke(null)));
        for (Object row : (List<?>) type.getMethod("capture",Class.forName("ka")).invoke(world,IglooProbeSupport.registryAccess())) {
            out.println("case " + gson.toJson(row));
        }
        for (Object row : (List<?>) type.getMethod("precipitationCases",Class.forName("ka")).invoke(world,IglooProbeSupport.registryAccess())) {
            out.println("precipitation " + gson.toJson(row));
        }
        for (Object row : (List<?>) type.getMethod("lightningCases",Class.forName("ka")).invoke(world,IglooProbeSupport.registryAccess())) {
            out.println("lightning " + gson.toJson(row));
        }
        out.flush();
    }
}
