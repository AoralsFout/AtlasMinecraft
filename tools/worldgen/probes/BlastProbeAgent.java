package atlasprobe;

import java.lang.instrument.*;
import java.security.ProtectionDomain;
import jdk.internal.org.objectweb.asm.*;

/** Invoke the fixture on the first dedicated-server tick, with its real registries and chunks. */
public final class BlastProbeAgent {
    private static boolean started;
    public static Object lastExplosion;

    public static void premain(String ignored, Instrumentation instrumentation) {
        instrumentation.addTransformer(new ClassFileTransformer() {
            public byte[] transform(ClassLoader loader, String name, Class<?> type,
                                    ProtectionDomain domain, byte[] bytes) {
                boolean level = name.equals("aqu");
                if (!level && !name.equals("apn")) return null;
                var reader = new ClassReader(bytes);
                var writer = new ClassWriter(reader, ClassWriter.COMPUTE_MAXS);
                reader.accept(new ClassVisitor(Opcodes.ASM8, writer) {
                    public MethodVisitor visitMethod(int access, String name, String descriptor,
                                                     String signature, String[] exceptions) {
                        var method = super.visitMethod(access, name, descriptor, signature, exceptions);
                        if (level && name.equals("a") && descriptor.endsWith(")Ldco;")) {
                            return new MethodVisitor(Opcodes.ASM8,method) {
                                public void visitInsn(int opcode) {
                                    if (opcode==Opcodes.ARETURN) {
                                        visitInsn(Opcodes.DUP);
                                        visitFieldInsn(Opcodes.PUTSTATIC,"atlasprobe/BlastProbeAgent","lastExplosion","Ljava/lang/Object;");
                                    }
                                    super.visitInsn(opcode);
                                }
                            };
                        }
                        if (level) return method;
                        if (!name.equals("c") || !descriptor.equals("(Ljava/util/function/BooleanSupplier;)V")) return method;
                        return new MethodVisitor(Opcodes.ASM8, method) {
                            public void visitCode() {
                                super.visitCode();
                                visitVarInsn(Opcodes.ALOAD, 0);
                                visitMethodInsn(Opcodes.INVOKESTATIC, "atlasprobe/BlastProbeAgent",
                                        "capture", "(Ljava/lang/Object;)V", false);
                            }
                        };
                    }
                }, 0);
                return writer.toByteArray();
            }
        });
    }

    public static void capture(Object server) {
        if (started) return;
        started = true;
        try {
            // The server JAR seals the default package.
            class Loader extends ClassLoader {
                Class<?> probe() throws Exception {
                    byte[] bytes = getResourceAsStream("BlastProbe.class").readAllBytes();
                    return defineClass("BlastProbe", bytes, 0, bytes.length);
                }
            }
            new Loader().probe().getMethod("capture", Object.class).invoke(null, server);
        } catch (Throwable error) {
            error.printStackTrace();
            System.exit(1);
        }
    }
}
