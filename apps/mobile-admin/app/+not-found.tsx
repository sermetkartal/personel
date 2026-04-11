import { Link, Stack } from "expo-router";
import { View, Text } from "react-native";

export default function NotFoundScreen() {
  return (
    <>
      <Stack.Screen options={{ title: "Sayfa Bulunamadı" }} />
      <View className="flex-1 items-center justify-center bg-surface-secondary p-8">
        <Text className="text-slate-900 text-xl font-bold mb-2">
          Sayfa Bulunamadı
        </Text>
        <Text className="text-slate-500 text-base text-center mb-6">
          Aradığınız sayfa mevcut değil.
        </Text>
        <Link href="/" className="text-brand-500 font-semibold text-base">
          Ana Sayfaya Dön
        </Link>
      </View>
    </>
  );
}
