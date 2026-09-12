# kotlinx.serialization generates serializers via reflection-free synthesized
# constructors; R8 needs the rules that ship with the runtime, plus the
# standard keep for @Serializable classes' generated companions.
-keepattributes *Annotation*, InnerClasses
-dontnote kotlinx.serialization.**

# Retrofit service methods are looked up reflectively.
-keepattributes Signature, Exceptions
-keepclassmembers,allowshrinking,allowobfuscation interface * {
    @retrofit2.http.* <methods>;
}
-dontwarn org.codehaus.mojo.animal_sniffer.IgnoreJRERequirement
-dontwarn javax.annotation.**
-dontwarn okhttp3.internal.platform.**
-dontwarn org.conscrypt.**
-dontwarn org.bouncycastle.**
-dontwarn org.openjsse.**
